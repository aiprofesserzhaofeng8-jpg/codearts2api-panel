package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/workbuddy"
)

func TestGrowthActivityWaitsForNativeProgressAndStopsOnCancellation(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		name := "asynchronous_scoring"
		if cancelEarly {
			name = "cancel_while_unconfirmed"
		}
		t.Run(name, func(t *testing.T) {
			f := newGrowthActionFixture()
			f.accepted["chat_5"], f.progress["chat_5"], f.ignoreProgress = true, 4, true
			reads := 0
			s, _, client := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
				f.mu.Lock()
				if r.URL.Path == workbuddy.GrowthTasksPath && f.reports > 0 {
					reads++
					if !cancelEarly && reads == 2 {
						f.progress["chat_5"] = 5
					}
				}
				f.mu.Unlock()
				f.serve(w, r)
			})
			addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
			budget := 10 * time.Second
			if cancelEarly {
				budget = 300 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			start := time.Now()
			reply, ready := s.runGrowthActivity(ctx, client, s.Store.Account("wb-cn").Credentials,
				provider.GrowthTask{Code: "chat_5", Current: 4, Target: 5}, 5, growthReply{})
			if ready == cancelEarly || reply.CompletedReports != 1 || f.reports != 1 || f.chats != 0 || f.claims != 0 {
				t.Fatal("unconfirmed activity repeated, claimed or billed", ready, reply, f.reports, f.chats, f.claims)
			}
			if cancelEarly && time.Since(start) > 2*time.Second || !cancelEarly && reads != 2 {
				t.Fatal("polling ignored cancellation or asynchronous progress", reads, time.Since(start))
			}
		})
	}
}
