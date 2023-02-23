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

	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/mir/pkg/client"
	mirproto "github.com/filecoin-project/mir/pkg/pb/requestpb"
	t "github.com/filecoin-project/mir/pkg/types"
)

const (
	ConfigurationRequestsDBPrefix = "mir/configuration/"
)

var (
	// NextConfigurationNumberKey is used to store SentConfigurationNumber
	// that is the maximum configuration request number (nonce) that has been sent.
	NextConfigurationNumberKey = datastore.NewKey("mir/next-config-number")
	// NextAppliedConfigurationNumberKey is used to store AppliedConfigurationNumber
	// that is the maximum configuration request number that has been applied.
	NextAppliedConfigurationNumberKey = datastore.NewKey("mir/next-applied-config-number")
	// ReconfigurationVotesKey is used to store configuration votes.
	ReconfigurationVotesKey = datastore.NewKey("mir/reconfiguration-votes")
)

var _ client.Client = &ConfigurationManager{}

type ConfigurationManager struct {
	ctx           context.Context // Parent context
	ds            db.DB           // Persistent storage.
	id            string          // Manager ID.
	nextReqNo     uint64          // The number that will be used in the next configuration Mir request.
	nextAppliedNo uint64          // The number of the next configuration Mir request that will be applied.
}

func NewConfigurationManager(ctx context.Context, ds db.DB, id string) (*ConfigurationManager, error) {
	cm := &ConfigurationManager{
		ctx:           ctx,
		ds:            ds,
		id:            id,
		nextReqNo:     0,
		nextAppliedNo: 0,
	}
	err := cm.recover()
	if err != nil {
		return nil, err
	}
	return cm, nil
}

// NewTX creates and returns a new configuration request with the next request number,
// corresponding to the number of transactions previously created by this client.
// Until Done is called with the returned request's number,
// the request will be pending, i.e., among the requests returned by Pending.
func (cm *ConfigurationManager) NewTX(_ uint64, data []byte) (*mirproto.Request, error) {
	r := mirproto.Request{
		ClientId: cm.id,
		ReqNo:    cm.nextReqNo,
		Type:     ConfigurationRequest,
		Data:     data,
	}

	if err := cm.storeRequest(&r, cm.nextReqNo); err != nil {
		log.With("validator", cm.id).Errorf("unable to store configuration request: %v", err)
		return nil, err
	}

	{
		// If a request with number n has been persisted and the node had crashed here
		// then when recovering the next configuration nonce can be n+1.
	}

	cm.nextReqNo++
	cm.storeNextConfigurationNumber(cm.nextReqNo)

	return &r, nil
}

// Done marks a configuration request as done. It will no longer be among the request returned by Pending.
func (cm *ConfigurationManager) Done(txNo t.ReqNo) error {
	cm.nextAppliedNo = uint64(txNo) + 1
	cm.storeNextAppliedConfigurationNumber(cm.nextAppliedNo)
	cm.removeRequest(uint64(txNo))
	return nil
}

// Pending returns from the persistent storage all requests previously returned by NewTX that have not been applied yet.
func (cm *ConfigurationManager) Pending() (reqs []*mirproto.Request, err error) {
	for i := cm.nextAppliedNo; i < cm.nextReqNo; i++ {
		r, err := cm.getRequest(i)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, r)
	}
	return
}

// Sync ensures that the effects of all previous calls to NewTX and Done have been written to persistent storage.
// We do not use this according to the used DB interface.
func (cm *ConfigurationManager) Sync() error {
	return nil
}

// recover function recovers configuration number, and configuration requests that may not be applied.
func (cm *ConfigurationManager) recover() error {
	nextReqNo := cm.getNextConfigurationNumber()
	appliedNumber := cm.getAppliedConfigurationNumber()

	if nextReqNo == appliedNumber && appliedNumber == 0 {
		return nil
	}
	if appliedNumber > nextReqNo {
		return fmt.Errorf("validator %v has incorrect configuration numbers: %d, %d", cm.id, appliedNumber, nextReqNo)
	}

	cm.nextAppliedNo = appliedNumber
	cm.nextReqNo = nextReqNo

	// If the node crashes immediately after the request with number n was persisted then the next configuration nonce can be
	// n+1. To distinguish that scenario we have to check the existence of n+1 request.
	_, err := cm.getRequest(nextReqNo + 1)
	switch {
	case errors.Is(err, datastore.ErrNotFound):
		return nil
	case err == nil:
		cm.nextReqNo++
		return nil
	case err != nil:
		return err
	}
	return nil
}

// storeRequest stores a configuration request and the corresponding configuration number in the persistent database.
func (cm *ConfigurationManager) storeRequest(r *mirproto.Request, n uint64) error {
	v, err := proto.Marshal(r)
	if err != nil {
		return err
	}
	return cm.ds.Put(cm.ctx, configurationIndexKey(n), v)
}

// getRequest gets a configuration request from the persistent database.
func (cm *ConfigurationManager) getRequest(n uint64) (*mirproto.Request, error) {
	b, err := cm.ds.Get(cm.ctx, configurationIndexKey(n))
	if err != nil {
		return nil, err
	}
	var r mirproto.Request
	if err := proto.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (cm *ConfigurationManager) removeRequest(n uint64) {
	if err := cm.ds.Delete(cm.ctx, configurationIndexKey(n)); err != nil {
		log.With("validator", cm.id).Warnf("failed to remove applied configuration request %d: %v", n, err)
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
	b, err := cm.ds.Get(cm.ctx, ReconfigurationVotesKey)
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
	recs := storeConfigurationVotes(votes)
	r := VoteRecords{
		Records: recs,
	}

	b := new(bytes.Buffer)
	if err := r.MarshalCBOR(b); err != nil {
		return err
	}
	if err := cm.ds.Put(cm.ctx, ReconfigurationVotesKey, b.Bytes()); err != nil {
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
	return datastore.NewKey(ConfigurationRequestsDBPrefix + strconv.FormatUint(n, 10))
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

func storeConfigurationVotes(votes map[uint64]map[string]map[t.NodeID]struct{}) []VoteRecord {
	var vs []VoteRecord
	for n, hashToValidatorsVotes := range votes {
		for h, nodeIDs := range hashToValidatorsVotes {
			e := VoteRecord{
				ConfigurationNumber: n,
				ValSetHash:          h,
			}
			for n, _ := range nodeIDs {
				e.VotedValidators = append(e.VotedValidators, VotedValidator{n.Pb()})
			}
			vs = append(vs, e)
		}
	}
	return vs
}
