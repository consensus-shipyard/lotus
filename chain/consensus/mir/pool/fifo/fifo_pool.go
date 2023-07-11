package fifo

import (
	"sync"

	"github.com/ipfs/go-cid"

	mirproto "github.com/filecoin-project/mir/pkg/pb/trantorpb/types"
)

// Pool is a structure to implement the simplest pool that enforces FIFO policy on client transactions.
// When a client sends a transaction we add clientID to the orderingClients and clientByCID maps.
// When we receive a transaction we find the clientID and remove it from the orderingClients.
// We don't need using sync primitives since the pool's methods are called only by one goroutine.
type Pool struct {
	clientByCID     map[cid.Cid]string // tx CID -> clientID
	orderingClients map[string]bool    // clientID -> bool
	seen            map[string]uint64  // clientID -> nonce
	lk              sync.RWMutex
}

func New() *Pool {
	return &Pool{
		clientByCID:     make(map[cid.Cid]string),
		orderingClients: make(map[string]bool),
		seen:            make(map[string]uint64),
	}
}

// AddTx adds the transaction if it satisfies to the FIFO policy.
func (p *Pool) AddTx(cid cid.Cid, r *mirproto.Transaction) (exist bool) {
	p.lk.Lock()
	defer p.lk.Unlock()
	_, exist = p.orderingClients[r.ClientId.Pb()]
	// If it doesn't exist, or it has a greater nonce than the one seen.
	if !exist || r.TxNo.Pb() > p.seen[r.ClientId.Pb()] {
		p.clientByCID[cid] = r.ClientId.Pb()
		p.orderingClients[r.ClientId.Pb()] = true
		// update last nonce seen
		p.seen[r.ClientId.Pb()] = r.TxNo.Pb()

	}
	return
}

// IsTargetTx returns whether the transaction with clientID should be sent or there is a transaction from that client that
// is in progress of ordering.
func (p *Pool) IsTargetTx(clientID string, nonce uint64) bool {
	p.lk.RLock()
	defer p.lk.RUnlock()
	_, inProgress := p.orderingClients[clientID]
	return !inProgress || nonce > p.seen[clientID]
}

// DeleteTx deletes the target transaction by the key h.
func (p *Pool) DeleteTx(cid cid.Cid, nonce uint64) (ok bool) {
	p.lk.Lock()
	defer p.lk.Unlock()
	clientID, ok := p.clientByCID[cid]
	if ok {
		delete(p.orderingClients, clientID)
		delete(p.clientByCID, cid)
		// if we are deleting no need to mark it as
		// seen as we have already seen it.
		return
	}
	// If we don't see it mark the transaction for that nonce
	// as seen by the pool, so we consider the last nonce proposed.
	p.seen[clientID] = nonce

	return
}

func (p *Pool) Purge() {
	p.lk.Lock()
	defer p.lk.Unlock()
	p.clientByCID = make(map[cid.Cid]string)
	p.orderingClients = make(map[string]bool)
	p.seen = make(map[string]uint64)
}
