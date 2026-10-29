package gateway

import (
	"context"
	"testing"
)

func TestTheServerFollowsTheNetworkSettingForRelayingBetweenDevices(t *testing.T) {
	c := newFakeControl(t)
	g, b, _ := newGateway(t, c, t.TempDir())
	ctx := context.Background()

	c.set(t, state(1)) // the setting is absent, which means off
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if on, set := b.Fake.Forwarding("wardline0"); !set || on {
		t.Fatalf("off must be set explicitly, not left to the host's default (set=%v on=%v)", set, on)
	}

	st := state(2)
	st.DevicesReachEachOther = true
	c.set(t, st)
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if on, _ := b.Fake.Forwarding("wardline0"); !on {
		t.Fatal("forwarding should be on")
	}

	c.set(t, state(3))
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if on, _ := b.Fake.Forwarding("wardline0"); on {
		t.Fatal("forwarding should be off again")
	}
}
