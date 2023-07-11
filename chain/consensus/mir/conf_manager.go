package mir

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/ipfs/go-datastore"
	"google.golang.org/protobuf/proto"

	"github.com/filecoin-project/mir/pkg/client"
	"github.com/filecoin-project/mir/pkg/pb/trantorpb"
	mirproto "github.com/filecoin-project/mir/pkg/pb/trantorpb/types"
	"github.com/filecoin-project/mir/pkg/trantor/types"
	t "github.com/filecoin-project/mir/pkg/types"
	"github.com/filecoin-project/mir/pkg/util/maputil"

	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
)

const (
	ConfigurationTxDBPrefix = "mir/configuration/"
)

var (
	// NextConfigurationNumberKey is used to store SentConfigurationNumber
	// that is the maximum configuration transaction number (nonce) that has been sent.
	NextConfigurationNumberKey = datastore.NewKey("mir/next-config-number")
	// NextAppliedConfigurationNumberKey is used to store AppliedConfigurationNumber
	// that is the maximum configuration transaction number that has been applied.
	NextAppliedConfigurationNumberKey = datastore.NewKey("mir/next-applied-config-number")
	// ConfigurationVotesKey is used to store configuration votes.
	ConfigurationVotesKey = datastore.NewKey("mir/reconfiguration-votes")
)

var _ client.Client = &ConfigurationManager{}

type ConfigurationManager struct {
	ctx                  context.Context // Parent context
	ds                   db.DB           // Persistent storage.
	id                   string          // Manager ID.
	nextTxNo             uint64          // The number that will be used in the next Mir configuration transaction.
	nextAppliedNo        uint64          // The number of the next configuration Mir transaction that will be applied.
	initialConfiguration membership.Info // Initial membership information.
}

func NewConfigurationManager(ctx context.Context, ds db.DB, id string) (*ConfigurationManager, error) {
	cm := &ConfigurationManager{
		ctx:                  ctx,
		ds:                   ds,
		id:                   id,
		nextTxNo:             0,
		nextAppliedNo:        0,
		initialConfiguration: membership.Info{},
	}
	err := cm.recover()
	if err != nil {
		return nil, err
	}
	return cm, nil
}

func NewConfigurationManagerWithMembershipInfo(ctx context.Context, ds db.DB, id string, info *membership.Info) (*ConfigurationManager, error) {
	cm := &ConfigurationManager{
		ctx:                  ctx,
		ds:                   ds,
		id:                   id,
		nextTxNo:             0,
		nextAppliedNo:        0,
		initialConfiguration: *info,
	}
	err := cm.recover()
	if err != nil {
		return nil, err
	}
	return cm, nil
}

// NewTX creates and returns a new configuration transaction with the next nextTxNo number,
// corresponding to the number of transactions previously created by this client.
// Until Done is called with the returned transaction number,
// the transaction will be pending, i.e., among the transactions returned by Pending.
func (cm *ConfigurationManager) NewTX(_ uint64, data []byte) (*mirproto.Transaction, error) {
	r := mirproto.Transaction{
		ClientId: types.ClientID(cm.id),
		TxNo:     types.TxNo(cm.nextTxNo),
		Type:     ConfigurationTransaction,
		Data:     data,
	}

	if err := cm.storeTx(&r, cm.nextTxNo); err != nil {
		log.With("validator", cm.id).Errorf("unable to store configuration tx: %v", err)
		return nil, err
	}

	{
		// If a transaction with number n was persisted and the node had crashed here
		// then when recovering the next configuration nonce can be n+1.
	}

	cm.nextTxNo++
	cm.storeNextConfigurationNumber(cm.nextTxNo)

	return &r, nil
}

func (cm *ConfigurationManager) GetInitialMembershipInfo() membership.Info {
	return cm.initialConfiguration
}

// Done marks a configuration transaction as done. It will no longer be among the transactions returned by Pending.
func (cm *ConfigurationManager) Done(txNo types.TxNo) error {
	cm.nextAppliedNo = txNo.Pb() + 1
	cm.storeNextAppliedConfigurationNumber(cm.nextAppliedNo)
	cm.removeTx(txNo.Pb())
	return nil
}

// Pending returns from the persistent storage all transactions previously returned by NewTX
// that have not been applied yet.
func (cm *ConfigurationManager) Pending() (txs []*mirproto.Transaction, err error) {
	for i := cm.nextAppliedNo; i < cm.nextTxNo; i++ {
		tx, err := cm.getTx(i)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

// Sync ensures that the effects of all previous calls to NewTX and Done have been written to persistent storage.
// We do not use this according to the used DB interface.
func (cm *ConfigurationManager) Sync() error {
	return nil
}

// recover function recovers configuration number, and configuration transactions that may not be applied.
func (cm *ConfigurationManager) recover() error {
	nextTxNo := cm.getNextConfigurationNumber()
	appliedNumber := cm.getAppliedConfigurationNumber()

	if nextTxNo == appliedNumber && appliedNumber == 0 {
		return nil
	}
	if appliedNumber > nextTxNo {
		return fmt.Errorf("validator %v has incorrect configuration numbers: %d, %d", cm.id, appliedNumber, nextTxNo)
	}

	cm.nextAppliedNo = appliedNumber
	cm.nextTxNo = nextTxNo

	// If the node crashes immediately after the transaction with number n was persisted then the next configuration nonce can be
	// n+1. To distinguish that scenario we have to check the existence of n+1 transaction.
	_, err := cm.getTx(nextTxNo + 1)
	switch {
	case errors.Is(err, datastore.ErrNotFound):
		return nil
	case err == nil:
		cm.nextTxNo++
		return nil
	case err != nil:
		return err
	}
	return nil
}

// storeTx stores a configuration transaction and the corresponding configuration number in the persistent database.
func (cm *ConfigurationManager) storeTx(r *mirproto.Transaction, n uint64) error {
	v, err := proto.Marshal(r.Pb())
	if err != nil {
		return err
	}
	return cm.ds.Put(cm.ctx, configurationIndexKey(n), v)
}

// getTx gets a configuration transaction from the persistent database.
func (cm *ConfigurationManager) getTx(n uint64) (*mirproto.Transaction, error) {
	b, err := cm.ds.Get(cm.ctx, configurationIndexKey(n))
	if err != nil {
		return nil, err
	}
	var r trantorpb.Transaction
	if err := proto.Unmarshal(b, &r); err != nil {
		return nil, err
	}

	return mirproto.TransactionFromPb(&r), nil
}

func (cm *ConfigurationManager) removeTx(n uint64) {
	if err := cm.ds.Delete(cm.ctx, configurationIndexKey(n)); err != nil {
		log.With("validator", cm.id).Warnf("failed to remove applied configuration tx %d: %v", n, err)
	}
}

func (cm *ConfigurationManager) storeNextConfigurationNumber(n uint64) {
	cm.storeNumber(NextConfigurationNumberKey, n)
}

func (cm *ConfigurationManager) storeNextAppliedConfigurationNumber(n uint64) {
	cm.storeNumber(NextAppliedConfigurationNumberKey, n)
}

func (cm *ConfigurationManager) getNextConfigurationNumber() uint64 {
	b, err := cm.ds.Get(cm.ctx, NextConfigurationNumberKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", cm.id).Info("stored next configuration number not found")
		return 0
	}
	if err != nil {
		log.With("validator", cm.id).Panic("failed to get next configuration number: %v", err)
	}
	return binary.LittleEndian.Uint64(b)
}

func (cm *ConfigurationManager) getAppliedConfigurationNumber() uint64 {
	b, err := cm.ds.Get(cm.ctx, NextAppliedConfigurationNumberKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", cm.id).Info("stored executed configuration number not found")
		return 0
	}
	if err != nil {
		log.With("validator", cm.id).Panic("failed to get applied configuration number: %v", err)
	}
	return binary.LittleEndian.Uint64(b)
}

func (cm *ConfigurationManager) GetConfigurationVotes() map[uint64]map[string]map[t.NodeID]struct{} {
	votes := make(map[uint64]map[string]map[t.NodeID]struct{})
	b, err := cm.ds.Get(cm.ctx, ConfigurationVotesKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", cm.id).Info("stored reconfiguration votes not found")
		return votes
	}
	if err != nil {
		log.With("validator", cm.id).Warnf("failed to get reconfiguration votes: %v", err)
		return votes
	}

	var r VoteRecords
	if err := r.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		log.With("validator", cm.id).Warnf("failed to unmarshal reconfiguration votes: %v", err)
		return votes
	}
	votes = GetConfigurationVotes(r.Records)

	return votes
}

func (cm *ConfigurationManager) StoreConfigurationVotes(votes map[uint64]map[string]map[t.NodeID]struct{}) error {
	recs := StoreConfigurationVotes(votes)
	r := VoteRecords{
		Records: recs,
	}

	b := new(bytes.Buffer)
	if err := r.MarshalCBOR(b); err != nil {
		return err
	}
	if err := cm.ds.Put(cm.ctx, ConfigurationVotesKey, b.Bytes()); err != nil {
		log.With("validator", cm.id).Warnf("failed to put reconfiguration votes: %v", err)
	}

	return nil
}

func (cm *ConfigurationManager) storeNumber(key datastore.Key, n uint64) {
	rb := make([]byte, 8)
	binary.LittleEndian.PutUint64(rb, n)
	if err := cm.ds.Put(cm.ctx, key, rb); err != nil {
		log.With("validator", cm.id).Warnf("failed to put configuration number by %s: %v", key, err)
	}
}

func configurationIndexKey(n uint64) datastore.Key {
	return datastore.NewKey(ConfigurationTxDBPrefix + strconv.FormatUint(n, 10))
}

func GetConfigurationVotes(vr []VoteRecord) map[uint64]map[string]map[t.NodeID]struct{} {
	m := make(map[uint64]map[string]map[t.NodeID]struct{})
	for _, v := range vr {
		if _, exist := m[v.ConfigurationNumber]; !exist {
			m[v.ConfigurationNumber] = make(map[string]map[t.NodeID]struct{})
		}
		for _, id := range v.VotedValidators {
			if _, exist := m[v.ConfigurationNumber][v.ValSetHash]; !exist {
				m[v.ConfigurationNumber][v.ValSetHash] = make(map[t.NodeID]struct{})
			}
			m[v.ConfigurationNumber][v.ValSetHash][t.NodeID(id.ID)] = struct{}{}
		}
	}
	return m
}

func StoreConfigurationVotes(votes map[uint64]map[string]map[t.NodeID]struct{}) []VoteRecord {
	var vs []VoteRecord

	for _, n := range maputil.GetSortedKeys(votes) {
		hashToValidatorsVotes := votes[n]
		for _, h := range maputil.GetSortedKeys(hashToValidatorsVotes) {
			nodeIDs := hashToValidatorsVotes[h]
			e := VoteRecord{
				ConfigurationNumber: n,
				ValSetHash:          h,
			}
			for _, n := range maputil.GetSortedKeys(nodeIDs) {
				e.VotedValidators = append(e.VotedValidators, VotedValidator{n.Pb()})
			}
			vs = append(vs, e)
		}
	}
	return vs
}
