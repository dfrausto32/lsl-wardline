package gateway

import (
	"context"
	"testing"
)

// Reach-all permission (task 6.9): the server is told which machines may reach every other machine.

func TestTheServerRelaysOnlyForTheMachinesThatMayReachEveryone(t *testing.T) {
	c := newFakeControl(t)
	g, b, _ := newGateway(t, c, t.TempDir())
	ctx := context.Background()

	st := state(1)
	st.ReachAll = []string{"10.77.5.9", "10.77.5.4"}
	c.set(t, st)
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	p, set := b.Fake.Relay("wardline0")
	if !set || p.All || len(p.ReachAll) != 2 || p.ReachAll[0].String() != "10.77.5.4" || p.ReachAll[1].String() != "10.77.5.9" {
		t.Fatalf("%+v set=%v", p, set)
	}

	// Taking the permission away is applied the same way, and leaves devices apart again.
	c.set(t, state(2))
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if p, _ := b.Fake.Relay("wardline0"); p.All || len(p.ReachAll) != 0 {
		t.Fatalf("the permission should be gone: %+v", p)
	}
	if on, _ := b.Fake.Forwarding("wardline0"); on {
		t.Fatal("with no permission and the setting off, nothing is relayed")
	}
}

func TestTheNetworkSettingStillMeansEveryMachineCanReachEveryOther(t *testing.T) {
	c := newFakeControl(t)
	g, b, _ := newGateway(t, c, t.TempDir())
	st := state(1)
	st.DevicesReachEachOther = true
	st.ReachAll = []string{"10.77.5.4"}
	c.set(t, st)
	if _, err := g.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p, _ := b.Fake.Relay("wardline0"); !p.All {
		t.Fatalf("%+v", p)
	}
}

func TestABadAddressInThePermissionListIsRefusedAndNothingChanges(t *testing.T) {
	c := newFakeControl(t)
	g, b, _ := newGateway(t, c, t.TempDir())
	st := state(1)
	st.ReachAll = []string{"not-an-address"}
	c.set(t, st)
	if _, err := g.Sync(context.Background()); err == nil {
		t.Fatal("an address that is not one must be refused")
	}
	if _, set := b.Fake.Relay("wardline0"); set {
		t.Fatal("nothing should have been applied")
	}
}
