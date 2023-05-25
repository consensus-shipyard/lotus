package fifo

import (
	"sync"

	"github.com/ipfs/go-cid"

	mirrequest "github.com/filecoin-project/mir/pkg/pb/requestpb"
)

// Pool is a structure to implement the simplest pool that enforces FIFO policy on client messages.
// When a client sends a message we add clientID to orderingClients map and clientByCID.
// When we receive a message we find the clientID and remove it from orderingClients.
// We don't need using sync primitives since the pool's methods are called only by one goroutine.
type Pool struct {
	clientByCID     map[cid.Cid]string // messageCID -> clientID
	orderingClients map[string]bool    // clientID -> bool
	seen            map[string]uint64  // clientID -> nonce
	delivered       map[string]uint64  // clientID -> nonce
	lk              sync.RWMutex
}

func New() *Pool {
	return &Pool{
		clientByCID:     make(map[cid.Cid]string),
		orderingClients: make(map[string]bool),
		seen:            make(map[string]uint64),
		delivered:       make(map[string]uint64),
	}
}

// AddRequest adds the request if it satisfies to the FIFO policy.
func (p *Pool) AddRequest(cid cid.Cid, r *mirrequest.Request) (exist bool) {
	p.lk.Lock()
	defer p.lk.Unlock()
	_, exist = p.orderingClients[r.ClientId]
	// if it doesn't exist or it has a greater nonce than the one seen.
	if !exist || (r.ReqNo == p.seen[r.ClientId]+1) {
		// TODO: The above condition is wrong, as it allows anything to be added if
		//  no request is stored in the fifo pool for this client ID. The condition expression should be
		//  (!exist && r.ReqNo == 0) || (r.ReqNo == p.seen[r.ClientId]+1)
		//  However, before using the correct expression, we must make sure that the proper state
		//  is loaded into p.seen (and p.delivered) upon validator restart.
		p.clientByCID[cid] = r.ClientId
		p.orderingClients[r.ClientId] = true
		// update last nonce seen
		p.seen[r.ClientId] = r.ReqNo

	}
	return
}

// IsTargetRequest returns whether the request with clientID should be sent or there is a request from that client that
// is in progress of ordering.
func (p *Pool) IsTargetRequest(clientID string, nonce uint64) bool {
	p.lk.RLock()
	defer p.lk.RUnlock()
	_, inProgress := p.orderingClients[clientID]
	return !inProgress || (nonce == p.seen[clientID]+1)
	// TODO: The above condition is wrong, as it allows anything to be added if
	//  no request is stored in the fifo pool for this client ID. The condition expression should be
	//  (!inProgress && nonce == 0) || (nonce == p.seen[clientID]+1)
	//  However, before using the correct expression, we must make sure that the proper state
	//  is loaded into p.seen (and p.delivered) upon validator restart.
}

// RequestDelivered deletes the target request by the key h.
func (p *Pool) RequestDelivered(cid cid.Cid, nonce uint64) (ok bool) {
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
	// if we don't see it mark the request for that nonce
	// as seen by the pool so we consider the last nonce proposed.
	// we also note the delivery of the request, for the purpose of purging "seen" at the end of the epoch.
	p.seen[clientID] = nonce
	p.delivered[clientID] = nonce

	return
}

func (p *Pool) Purge() {
	p.lk.Lock()
	defer p.lk.Unlock()
	p.clientByCID = make(map[cid.Cid]string)
	p.orderingClients = make(map[string]bool)
	p.seen = make(map[string]uint64)
	// reset the "seen" values to what was delivered.
	for clientID, nonce := range p.delivered {
		p.seen[clientID] = nonce
	}
}
