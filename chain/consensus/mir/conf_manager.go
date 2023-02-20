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
	// AppliedConfigurationNumberKey is used to store AppliedConfigurationNumber
	// that is the maximum configuration request number that has been applied.
	AppliedConfigurationNumberKey = datastore.NewKey("mir/applied-config-number")
	// ReconfigurationVotesKey is used to store configuration votes.
	ReconfigurationVotesKey = datastore.NewKey("mir/reconfiguration-votes")
)

type ConfigurationManager struct {
	ctx context.Context // Parent context
	ds  db.DB           // Persistent storage.
	id  string          // Parent ID.
}

func NewConfigurationManager(ctx context.Context, ds db.DB, id string) *ConfigurationManager {
	return &ConfigurationManager{
		ctx: ctx,
		ds:  ds,
		id:  id,
	}
}

// GetConfigurationData recovers configuration number, and configuration requests that may be not applied.
func (c *ConfigurationManager) GetConfigurationData() (reqs []*mirproto.Request, nextReqNo uint64, err error) {
	// o is the offset to distinguish cases when there are no applied configuration requests and when the
	// applied reconfiguration request number is 0.
	// Since we use uint64 we cannot return -1 when there are no applied configurations.
	o := uint64(1)

	nextReqNo = c.GetNextConfigurationNumber()

	appliedNumber, found := c.GetAppliedConfigurationNumber()
	if !found {
		o = 0
	}

	if nextReqNo == appliedNumber && nextReqNo == 0 {
		return nil, 0, nil
	}
	if appliedNumber > nextReqNo {
		return nil, 0, fmt.Errorf("validator %v has incorrect configuration numbers: %d, %d", c.id, appliedNumber, nextReqNo)
	}

	for i := appliedNumber + o; i < nextReqNo; i++ {
		r, err := c.GetConfigurationRequest(i)
		if err != nil {
			return nil, 0, err
		}
		reqs = append(reqs, r)
	}

	// If a node crashes right after we put a configuration request the actual configuration nonce
	// can be equal to GetNextConfigurationNumber + 1.
	r, err := c.GetConfigurationRequest(nextReqNo)
	switch {
	case errors.Is(err, datastore.ErrNotFound):
		return reqs, nextReqNo, nil
	case err == nil:
		return append(reqs, r), nextReqNo + 1, nil
	case err != nil:
		return nil, 0, err
	}
	return
}

// StoreConfigurationRequest stores a configuration request and the corresponding configuration number in the persistent database.
func (c *ConfigurationManager) StoreConfigurationRequest(r *mirproto.Request, n uint64) error {
	v, err := proto.Marshal(r)
	if err != nil {
		return err
	}
	return c.ds.Put(c.ctx, configurationIndexKey(n), v)
}

// GetConfigurationRequest gets a configuration request from the persistent database.
func (c *ConfigurationManager) GetConfigurationRequest(n uint64) (*mirproto.Request, error) {
	b, err := c.ds.Get(c.ctx, configurationIndexKey(n))
	if err != nil {
		return nil, err
	}
	var r mirproto.Request
	if err := proto.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *ConfigurationManager) RemoveConfigurationRequest(n uint64) {
	if err := c.ds.Delete(c.ctx, configurationIndexKey(n)); err != nil {
		log.With("validator", c.id).Warnf("failed to remove applied configuration request %d: %v", n, err)
	}
}

func (c *ConfigurationManager) StoreNextConfigurationNumber(n uint64) {
	c.storeNumber(NextConfigurationNumberKey, n)
}

func (c *ConfigurationManager) StoreAppliedConfigurationNumber(n uint64) {
	c.storeNumber(AppliedConfigurationNumberKey, n)
}

func (c *ConfigurationManager) GetNextConfigurationNumber() uint64 {
	b, err := c.ds.Get(c.ctx, NextConfigurationNumberKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", c.id).Info("stored next configuration number not found")
		return 0
	}
	if err != nil {
		log.With("validator", c.id).Panic("failed to get next configuration number: %v", err)
	}
	return binary.LittleEndian.Uint64(b)
}

func (c *ConfigurationManager) GetAppliedConfigurationNumber() (n uint64, found bool) {
	found = true
	b, err := c.ds.Get(c.ctx, AppliedConfigurationNumberKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", c.id).Info("stored executed configuration number not found")
		return 0, !found
	}
	if err != nil {
		log.With("validator", c.id).Panic("failed to get applied configuration number: %v", err)
	}
	return binary.LittleEndian.Uint64(b), found
}

func (c *ConfigurationManager) GetReconfigurationVotes() map[uint64]map[string][]t.NodeID {
	votes := make(map[uint64]map[string][]t.NodeID)
	b, err := c.ds.Get(c.ctx, ReconfigurationVotesKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", c.id).Info("stored reconfiguration votes not found")
		return votes
	}
	if err != nil {
		log.With("validator", c.id).Warnf("failed to get reconfiguration votes: %v", err)
		return votes
	}

	var r VoteRecords
	if err := r.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		log.With("validator", c.id).Warnf("failed to unmarshal reconfiguration votes: %v", err)
		return votes
	}
	votes = GetConfigurationVotes(r.Records)

	return votes
}

func (c *ConfigurationManager) StoreReconfigurationVotes(votes map[uint64]map[string][]t.NodeID) error {
	recs := StoreConfigurationVotes(votes)
	r := VoteRecords{
		Records: recs,
	}

	b := new(bytes.Buffer)
	if err := r.MarshalCBOR(b); err != nil {
		return err
	}
	if err := c.ds.Put(c.ctx, ReconfigurationVotesKey, b.Bytes()); err != nil {
		log.With("validator", c.id).Warnf("failed to put reconfiguration votes: %v", err)
	}

	return nil
}

func (c *ConfigurationManager) storeNumber(key datastore.Key, n uint64) {
	rb := make([]byte, 8)
	binary.LittleEndian.PutUint64(rb, n)
	if err := c.ds.Put(c.ctx, key, rb); err != nil {
		log.With("validator", c.id).Warnf("failed to put configuration number by %s: %v", key, err)
	}
}

func configurationIndexKey(n uint64) datastore.Key {
	return datastore.NewKey(ConfigurationRequestsDBPrefix + strconv.FormatUint(n, 10))
}

func GetConfigurationVotes(vr []VoteRecord) map[uint64]map[string][]t.NodeID {
	m := make(map[uint64]map[string][]t.NodeID)
	for _, v := range vr {
		if _, exist := m[v.ConfigurationNumber]; !exist {
			m[v.ConfigurationNumber] = make(map[string][]t.NodeID)
		}
		for _, id := range v.VotedValidators {
			m[v.ConfigurationNumber][v.ValSetHash] = append(m[v.ConfigurationNumber][v.ValSetHash], id.NodeID())
		}
	}
	return m
}

func StoreConfigurationVotes(reconfigurationVotes map[uint64]map[string][]t.NodeID) []VoteRecord {
	var vs []VoteRecord
	for n, hashToValidatorsVotes := range reconfigurationVotes {
		for h, nodeIDs := range hashToValidatorsVotes {
			e := VoteRecord{
				ConfigurationNumber: n,
				ValSetHash:          h,
				VotedValidators:     NewVotedValidators(nodeIDs...),
			}
			vs = append(vs, e)
		}
	}
	return vs
}
