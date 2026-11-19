package pubsub

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
)

func newTestPubSub(t *testing.T) *PubSub {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	h, err := libp2p.New(libp2p.NoListenAddrs, libp2p.DisableRelay())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	p, err := New(ctx, h)
	if err != nil {
		cancel()
		h.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		p.Close()
		h.Close()
	})
	return p
}

func TestPublishBeforeJoinCanSubscribe(t *testing.T) {
	p := newTestPubSub(t)
	const topic = "publish-before-subscribe"
	if err := p.Publish(topic, []byte("before subscription")); err != nil {
		t.Fatal(err)
	}
	sub, err := p.Join(topic)
	if err != nil {
		t.Fatalf("subscribe after auto-join: %v", err)
	}
	if p.Subscription(topic) != sub {
		t.Fatal("subscription was not cached")
	}
	want := []byte("after subscription")
	if err := p.Publish(topic, want); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msg.Data, want) {
		t.Fatalf("received %q, want %q", msg.Data, want)
	}
}

func TestConcurrentFirstPublishUsesOneTopic(t *testing.T) {
	p := newTestPubSub(t)
	const workers = 64
	start := make(chan struct{})
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errors <- p.Publish("concurrent-first-publish", []byte(fmt.Sprintf("message-%d", i)))
		}(i)
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Errorf("concurrent publish: %v", err)
		}
	}
	if _, err := p.Join("concurrent-first-publish"); err != nil {
		t.Fatalf("subscribe to published topic: %v", err)
	}
}

func TestJoinIsIdempotentAndCloseCancelsSubscription(t *testing.T) {
	p := newTestPubSub(t)
	const topic = "subscription-lifecycle"
	sub, err := p.Join(topic)
	if err != nil {
		t.Fatal(err)
	}
	again, err := p.Join(topic)
	if err != nil || again != sub {
		t.Fatalf("repeated Join returned a different subscription: %v", err)
	}
	p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sub.Next(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("closed subscription did not cancel immediately: %v", err)
	}
	if p.Subscription(topic) != nil || len(p.ListPeers(topic)) != 0 {
		t.Fatal("Close retained topic or subscription state")
	}
	p.Close()
}
