package reign

import (
	"testing"

	"github.com/thejerf/reign/internal"
)

func TestConnectionStatusCallback(t *testing.T) {
	connection, _ := noClustering(NullLogger)
	r := newRegistry(connection, connection.nodeID)
	r.connectionStatusCallback(connection.nodeID, false)
}

func TestNoServe(t *testing.T) {
	connection, _ := noClustering(NullLogger)
	addr, mbx := connection.NewMailbox()

	addr.Send(void)
	mbx.ReceiveNext()
}

func TestLookup(t *testing.T) {
	connection, r := noClustering(NullLogger)
	addr, mbx := connection.NewMailbox()
	go func() { r.Serve() }()
	defer r.Stop()

	name := "name"
	r.Register(name, addr)
	r.Sync()
	a := r.Lookup(name)
	a.Send(void)
	mbx.ReceiveNext()
}

func TestConnectionStatus(t *testing.T) {
	connection, r := noClustering(NullLogger)
	addr, mbx := connection.NewMailbox()
	go func() { r.Serve() }()
	defer r.Stop()

	// This shouldn't do anything, since connected == true
	cs := connectionStatus{connection.nodeID, true}
	r.send(cs)

	// First register a mailbox
	name := "blah"
	r.Register(name, addr)

	// This should unregister everything on this node.
	cs.connected = false
	r.send(cs)
	r.Sync()

	// Now make sure the mailbox was unregistered by re-registering with the same info (if unregister
	// was never called, this would panic)
	r.Register(name, addr)
	r.Sync()
	addr.Send(void)

	// mbx should not have received a MultipleClaim
	v := mbx.ReceiveNext()
	if _, ok := v.(voidtype); !ok {
		t.Fatalf("Did not expect a %t message: ", v)
	}
}

func TestUnregisterOnTerminate(t *testing.T) {
	connection, r := noClustering(NullLogger)
	go func() { r.Serve() }()

	addr, mbx := connection.NewMailbox()
	names := []string{"blah", "blech", "blorg"}

	for _, name := range names {
		r.Register(name, addr)
	}
	r.Sync()

	// Make sure Lookup works
	for _, name := range names {
		addr := r.Lookup(name)
		if addr == nil {
			t.Fatalf("Lookup failed on registered name '%s'", name)
		}
	}

	// Terminate() should call r.UnregisterMailbox and unregistered all the names that
	// belong to mbx
	mbx.Terminate()
	r.Sync()

	// Now make sure that all the names have been unregistered
	for _, name := range names {
		addr := r.Lookup(name)
		if addr != nil {
			t.Fatalf("Terminate() did not unregister name '%s'", name)
		}
	}
}

func TestInternalRegisterName(t *testing.T) {
	connection, r := noClustering(NullLogger)
	go func() { r.Serve() }()
	defer r.Stop()

	addr1, mbx1 := connection.NewMailbox()
	addr2, mbx2 := connection.NewMailbox()

	name := "blah"
	r.Register(name, addr1)
	r.Register(name, addr2)
	// 255 is nonexistent node, but we're only looking at the messages sent to mbx1. This
	// should trigger a MultiplClaim broadcast to mbx2 (invalid) and mbx1 (valid)
	r.Sync()

	addr1.Send(void)
	addr2.Send(void)

	// Make sure we received the MultipleClaim
	mc := mbx1.ReceiveNext()
	if _, ok := mc.(MultipleClaim); !ok {
		t.Fatal("Register is supposed to trigger a register")
	}
	mc = mbx2.ReceiveNext()
	if _, ok := mc.(MultipleClaim); !ok {
		t.Fatal("Register is supposed to trigger a register")
	}
}

func TestInternalUnegisterName(t *testing.T) {
	connection, r := noClustering(NullLogger)
	go func() { r.Serve() }()
	defer r.Stop()

	name := "blah"
	fakeName := "blorg"

	addr, mbx := connection.NewMailbox()
	err := r.Register(name, addr)

	if err != nil {
		t.Fatal("Initial Register should have succeeded")
	}

	r.Sync()

	// If this Unregsister doesn't work, the second Register will trigger a MultipleClaim broadcast
	r.Unregister(name, addr)
	r.Sync()
	addr.Send(void)

	// mbx should not have received a MultipleClaim
	v := mbx.ReceiveNext()
	if _, ok := v.(voidtype); !ok {
		t.Fatalf("Did not expect a %#v message: ", v)
	}

	// Unregistering a fake entry should do nothing
	r.Unregister(fakeName, addr)
	r.Sync()
}

func TestInternalAllNodeClaims(t *testing.T) {
	connection, r := noClustering(NullLogger)
	go func() { r.Serve() }()
	defer r.Stop()

	// Make a bunch of names for the same mailbox and register them with AllNodeClaims
	names := []string{"foo", "bar", "baz"}
	addr1, mbx1 := connection.NewMailbox()
	mid := internal.IntMailboxID(mbx1.id)
	registrationMap := make(map[string]internal.IntMailboxID)
	for _, name := range names {
		registrationMap[name] = mid
	}
	anc := internal.AllNodeClaims{
		Node:   internal.IntNodeID(connection.nodeID),
		Claims: registrationMap,
	}
	r.send(anc)

	r.Sync()

	// Now register all of the previous names again, but to a different mailbox.
	// This should trigger a MultipleClaim broadcast
	addr2, mbx2 := connection.NewMailbox()
	for _, name := range names {
		r.Register(name, addr2)
	}
	r.Sync()

	// Make sure we don't block
	for _ = range names {
		addr1.Send(void)
	}
	for _ = range names {
		addr2.Send(void)
	}

	// Verify that we got MultipleClaims on both mailboxen
	for _, mbx := range []*Mailbox{mbx1, mbx2} {
		for _, name := range names {
			mc := mbx.ReceiveNext()
			if _, ok := mc.(MultipleClaim); !ok {
				t.Fatalf("Did not expect message of type %#v", mc)
			} else {
				if mc.(MultipleClaim).name != name {
					t.Fatalf("Expected name to be '%v'. Got '%v'", name, mc.(MultipleClaim).name)
				}
			}
		}
	}
}
