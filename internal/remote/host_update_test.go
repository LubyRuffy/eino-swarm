package remote

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/update"
	"github.com/LubyRuffy/pairlink/client"
)

const (
	confirmedHostUpdate = "2014ab4f-2588-4b77-a709-c71273a535e0"
	anotherHostUpdate   = "c5ada69a-24e5-492a-8bf9-6f834188427c"
)

// Exercise the real binding, relay, sealed messages and Host-owned receive
// loop. A blocked release lookup or download must not starve other phone RPCs.
func TestPairedHostUpdateStaysOwnedAcrossReconnect(t *testing.T) {
	e := testEngine(t)
	r := newHostLinkRelay(t, e)
	r.host.SetVersion("2.1.0")
	p := r.phone(t)
	if got := hostUpdateRPC(t, p, Request{ID: "before", Op: OpMeta}); !got.OK || got.HostUpdateSupported {
		t.Fatalf("non-updatable host metadata: %+v", got)
	}
	checkStarted, allowCheck := make(chan struct{}), make(chan struct{})
	installStarted, allowInstall := make(chan struct{}), make(chan struct{})
	var installs atomic.Int32
	svc := update.NewHostService(t.Context(), e.Config().DataDir(), "2.1.0", update.HostUpdateOptions{
		Eligible: func() bool { return true },
		Check: func(ctx context.Context, fresh bool) update.Result {
			if !fresh {
				t.Error("explicit phone check lost its freshness request")
			}
			close(checkStarted)
			select {
			case <-allowCheck:
				return update.Result{Status: "available", Current: "2.1.0", Offer: &update.Offer{
					Version: "2.2.0", AssetURL: "https://example.invalid/host-only-url",
				}}
			case <-ctx.Done():
				return update.Result{Status: "error", Message: ctx.Err().Error()}
			}
		},
		Install: func(ctx context.Context, job *update.HostJob) error {
			installs.Add(1)
			if err := job.SetStage("downloading"); err != nil {
				return err
			}
			job.ReportDownload(11, 19)
			close(installStarted)
			select {
			case <-allowInstall:
				return job.SetStage("completed")
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}, func() (func(), error) { return func() {}, nil }, slog.New(slog.DiscardHandler))
	t.Cleanup(svc.Close)
	r.host.SetHostUpdater(svc)

	if got := hostUpdateRPC(t, p, Request{ID: "capability", Op: OpMeta}); !got.HostUpdateSupported || got.HostVersion != "2.1.0" {
		t.Fatalf("upgrade capability on existing link: %+v", got)
	}
	p.send(t, Request{ID: "offer", Op: OpHostUpdateCheck, Fresh: true})
	awaitHostUpdateSignal(t, checkStarted)
	if got := hostUpdateRPC(t, p, Request{ID: "check-again", Op: OpHostUpdateCheck}); got.Code != "update_busy" {
		t.Fatalf("a redundant check consumed a second request: %+v", got)
	}
	if got := hostUpdateRPC(t, p, Request{ID: "missing", Op: OpHostUpdateStatus, OperationID: confirmedHostUpdate}); got.OK || got.Code != "not_found" {
		t.Fatalf("status while release check is blocked: %+v", got)
	}
	close(allowCheck)
	offer := p.receive(t, func(r Response) bool { return r.ID == "offer" })
	if offer.HostUpdateOffer == nil || offer.HostUpdateOffer.Offer == nil || offer.HostUpdateOffer.Offer.Version != "2.2.0" {
		t.Fatalf("host update offer: %+v", offer)
	}
	wire, err := json.Marshal(offer)
	if err != nil || strings.Contains(string(wire), "host-only-url") {
		t.Fatalf("offer projected a host-only download URL: %s, %v", wire, err)
	}
	req := Request{ID: "confirm", Op: OpHostUpdateStart, Version: "2.2.0", OperationID: confirmedHostUpdate}
	if got := hostUpdateRPC(t, p, req); !got.OK || got.HostUpdate == nil || got.HostUpdate.Stage != "checking" {
		t.Fatalf("confirmation did not acknowledge ownership: %+v", got)
	}
	awaitHostUpdateSignal(t, installStarted)
	old := currentHostLink(t, r.host, p)
	r.reconnect(t, p)
	got := hostUpdateRPC(t, p, Request{ID: "progress", Op: OpHostUpdateStatus, OperationID: confirmedHostUpdate})
	waitHostLinkExit(t, old)
	if got.HostUpdate == nil || got.HostUpdate.Stage != "downloading" || got.HostUpdate.DownloadedBytes != 11 || got.Startup != "ready" {
		t.Fatalf("reconnected status: %+v", got)
	}
	req.ID = "replay"
	if replay := hostUpdateRPC(t, p, req); !replay.OK || replay.HostUpdate.DownloadedBytes != 11 || installs.Load() != 1 {
		t.Fatalf("replay started again: %+v, installs=%d", replay, installs.Load())
	}
	if busy := hostUpdateRPC(t, p, Request{ID: "parallel-install", Op: OpHostUpdateStart,
		OperationID: anotherHostUpdate, Version: "2.2.0"}); busy.Code != "update_busy" {
		t.Fatalf("during download, an independent confirmation was not refused: %+v", busy)
	}

	// A revoked ticket cannot submit another operation over the relay or dial
	// back in. The earlier confirmed operation still belongs to the host.
	bindings, err := r.host.ListBindings(t.Context())
	if err != nil || len(bindings) != 1 {
		t.Fatalf("binding = %+v, %v", bindings, err)
	}
	if err := r.host.RevokeBinding(t.Context(), bindings[0].ID); err != nil {
		t.Fatal(err)
	}
	revoked, err := client.Dial(t.Context(), client.Config{HubURL: r.url, Identity: p.id, Token: p.ticket, DisableUDP: true})
	if err == nil {
		_ = revoked.Close()
		t.Fatal("revoked pairing could dial back")
	}
	req.ID, req.OperationID = "revoked", anotherHostUpdate
	packet, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = p.link.Send(packet) // A closing socket may accept a write locally.
	close(allowInstall)
	for deadline := time.Now().Add(5 * time.Second); ; {
		done, err := svc.Status(confirmedHostUpdate)
		if err == nil && done.Stage == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("host lost its accepted operation: %+v, %v", done, err)
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := svc.Status(anotherHostUpdate); !errors.Is(err, update.ErrHostUpdateNotFound) {
		t.Fatalf("revoked phone created a job: %v", err)
	}
}

func TestHostUpdateRequiresReadyEngineAndCurrentPairedLink(t *testing.T) {
	e := testEngine(t)
	r := newHostLinkRelay(t, e)
	p := r.phone(t)
	unknownVersion := []byte(`{"v":99,"id":"bad-protocol","op":"host_update_start"}`)
	if err := p.link.Send(unknownVersion); err != nil {
		t.Fatal(err)
	}
	if got := p.receive(t, func(r Response) bool { return r.ID == "bad-protocol" }); got.Code != "bad_version" {
		t.Fatalf("unknown protocol version admitted update: %+v", got)
	}
	start := Request{ID: "without-updater", Op: OpHostUpdateStart, OperationID: confirmedHostUpdate, Version: "2.2.0"}
	if got := hostUpdateRPC(t, p, start); got.Code != "update_unsupported" {
		t.Fatalf("unsupported launch admitted update: %+v", got)
	}
	svc := update.NewHostService(t.Context(), e.Config().DataDir(), "2.1.0", update.HostUpdateOptions{
		Eligible: func() bool { return true },
		Install: func(context.Context, *update.HostJob) error {
			t.Error("update admitted during engine restore")
			return errors.New("unexpected install")
		},
	}, func() (func(), error) { return func() {}, nil }, slog.New(slog.DiscardHandler))
	t.Cleanup(svc.Close)
	r.host.SetHostUpdater(svc)
	if got := hostUpdateRPC(t, p, Request{ID: "invalid-status", Op: OpHostUpdateStatus, OperationID: "../outside"}); got.Code != "bad_request" {
		t.Fatalf("invalid status identity admitted: %+v", got)
	}
	e.BeginRestore()
	start.ID = "restoring"
	if got := hostUpdateRPC(t, p, start); got.Code != "restoring" {
		t.Fatalf("restoring engine admitted update: %+v", got)
	}
	if _, err := svc.Status(confirmedHostUpdate); !errors.Is(err, update.ErrHostUpdateNotFound) {
		t.Fatalf("restoring engine accepted a job: %v", err)
	}
	if got := Handle(e, e.Config().Remote, start, "relay", "unpaired"); got.OK {
		t.Fatalf("raw dispatch bypassed the binding gate: %+v", got)
	}
}

func TestHostUpdateReconnectCancelsTheObsoleteReleaseCheck(t *testing.T) {
	e := testEngine(t)
	r := newHostLinkRelay(t, e)
	p := r.phone(t)
	checking, cancelled := make(chan struct{}), make(chan struct{})
	svc := update.NewHostService(t.Context(), e.Config().DataDir(), "2.1.0", update.HostUpdateOptions{
		Eligible: func() bool { return true },
		Check: func(ctx context.Context, _ bool) update.Result {
			close(checking)
			<-ctx.Done()
			close(cancelled)
			return update.Result{Status: "error"}
		},
	}, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(svc.Close)
	r.host.SetHostUpdater(svc)
	p.send(t, Request{ID: "check-then-disconnect", Op: OpHostUpdateCheck})
	awaitHostUpdateSignal(t, checking)
	old := currentHostLink(t, r.host, p)
	r.reconnect(t, p)
	if got := hostUpdateRPC(t, p, Request{ID: "meta-after-disconnect", Op: OpMeta}); !got.HostUpdateSupported {
		t.Fatalf("the replacement link lost its update capability: %+v", got)
	}
	awaitHostUpdateSignal(t, cancelled)
	waitHostLinkExit(t, old)
}

func hostUpdateRPC(t *testing.T, phone *hostLinkPhone, req Request) Response {
	t.Helper()
	phone.send(t, req)
	return phone.receive(t, func(r Response) bool { return r.ID == req.ID })
}

func awaitHostUpdateSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("host update did not reach the expected stage")
	}
}
