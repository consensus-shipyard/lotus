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
	// SentConfigurationNumberKey is used to store SentConfigurationNumber
	// that is the maximum configuration request number (nonce) that has been sent.
	SentConfigurationNumberKey = datastore.NewKey("mir/sent-config-number")
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

// GetConfigurationData recovers configuration related data from the persistent database.
//
// It recovers configuration number, and configuration requests that may be not applied.
func (c *ConfigurationManager) GetConfigurationData() ([]*mirproto.Request, uint64, error) {
	// o is the offset to distinguish cases when there are no applied configuration requests and when the
	// applied reconfiguration request number is 0.
	// Since we use uint64 we cannot return -1 when there are no applied configurations.
	o := uint64(1)

	sentNumber := c.GetSentConfigurationNumber()
	appliedNumber, found := c.GetAppliedConfigurationNumber()
	if !found {
		o = 0
	}

	if sentNumber == appliedNumber && sentNumber == 0 {
		return nil, 0, nil
	}
	if appliedNumber > sentNumber {
		return nil, 0, fmt.Errorf("validator %v has incorrect configuration numbers: %d, %d", c.id, appliedNumber, sentNumber)
	}

	// Check do we need recovering configuration data or not.
	var configRequests []*mirproto.Request

	for i := appliedNumber + o; i <= sentNumber; i++ {
		b, err := c.ds.Get(c.ctx, configurationIndexKey(i))
		if err != nil {
			return nil, 0, err
		}

		r := mirproto.Request{}
		err = proto.Unmarshal(b, &r)
		if err != nil {
			log.With("validator", c.id).Errorf("unable to marshall configuration request: %v", err)
			return nil, 0, err
		}

		configRequests = append(configRequests, &r)
	}

	return configRequests, sentNumber, nil
}

// StoreConfigurationRequest stored a configuration request and the corresponding configuration number in the persistent database.
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

func (c *ConfigurationManager) RemoveConfigurationRequest(nonce uint64) {
	if err := c.ds.Delete(c.ctx, configurationIndexKey(nonce)); err != nil {
		log.With("validator", c.id).Warnf("failed to remove applied configuration request %d: %v", nonce, err)
	}
}

func (c *ConfigurationManager) StoreSentConfigurationNumber(nonce uint64) {
	c.storeNumber(SentConfigurationNumberKey, nonce)
}

func (c *ConfigurationManager) StoreAppliedConfigurationNumber(nonce uint64) {
	c.storeNumber(AppliedConfigurationNumberKey, nonce)
}

func (c *ConfigurationManager) GetSentConfigurationNumber() uint64 {
	b, err := c.ds.Get(c.ctx, SentConfigurationNumberKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", c.id).Info("stored sent configuration number not found")
		return 0
	}
	if err != nil {
		log.With("validator", c.id).Warnf("failed to get sent configuration number: %v", err)
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

func (c *ConfigurationManager) GetAppliedConfigurationNumber() (uint64, bool) {
	found := true
	b, err := c.ds.Get(c.ctx, AppliedConfigurationNumberKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", c.id).Info("stored executed configuration number not found")
		return 0, !found
	}
	if err != nil {
		log.With("validator", c.id).Warnf("failed to get applied configuration number: %v", err)
		return 0, !found
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

func configurationIndexKey(nonce uint64) datastore.Key {
	return datastore.NewKey(ConfigurationRequestsDBPrefix + strconv.FormatUint(nonce, 10))
}

func GetConfigurationVotes(voteRecords []VoteRecord) map[uint64]map[string][]t.NodeID {
	m := make(map[uint64]map[string][]t.NodeID)
	for _, v := range voteRecords {
		if _, exist := m[v.ConfigurationNumber]; !exist {
			m[v.ConfigurationNumber] = make(map[string][]t.NodeID)
		}
		for _, id := range v.VotedValidators {
			m[v.ConfigurationNumber][v.ValSetHash] = append(m[v.ConfigurationNumber][v.ValSetHash], id.NodeID())
		}
	}
	return m
}

func StoreConfigurationVotes(reconfigurationVotes map[uint64]map[string][]t.NodeID) (votesRecords []VoteRecord) {
	for n, hashToValidatorsVotes := range reconfigurationVotes {
		for h, nodeIDs := range hashToValidatorsVotes {
			e := VoteRecord{
				ConfigurationNumber: n,
				ValSetHash:          h,
				VotedValidators:     NewVotedValidators(nodeIDs...),
			}
			votesRecords = append(votesRecords, e)
		}
	}
	return
}
