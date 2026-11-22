package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
)

type subRefRemovalSummary struct {
	SubscriptionID   int64             `json:"subscriptionId"`
	SubscriptionName string            `json:"subscriptionName"`
	LandingNames     []string          `json:"landingNames,omitempty"`
	RelayNames       []string          `json:"relayNames,omitempty"`
	NoneNames        []string          `json:"noneNames,omitempty"`
	Bindings         map[string]string `json:"bindings,omitempty"`
}

func (h *Handler) knownChainEndpointRefs() (map[string]bool, map[string]bool, error) {
	rows, err := h.repo.ListNodeInbounds()
	if err != nil {
		return nil, nil, err
	}
	relays, err := h.relayViews(0)
	if err != nil {
		return nil, nil, err
	}
	activeNames := make(map[string]bool, len(rows)+len(relays))
	activeIDs := make(map[string]bool, len(rows))
	for _, row := range rows {
		activeIDs[strconv.FormatInt(row.ID, 10)] = true
		activeNames[row.Name] = true
		if inboundShareMode(&row) == "dual" {
			activeNames[row.Name+dualStackV6Suffix] = true
		}
	}
	for _, rel := range relays {
		activeNames[rel.Name] = true
		for i := range rel.Entries {
			if len(rel.Entries) > 1 {
				activeNames[fmt.Sprintf("%s %d", rel.Name, i+1)] = true
			}
		}
	}
	return activeNames, activeIDs, nil
}

// syncSubscriptionInboundRename rewrites stored chain references when an
// inbound or relay changes its name (or exits dual-stack mode).
func (h *Handler) syncSubscriptionInboundRename(oldName, newName string, wasDual, isDual bool) {
	if oldName == "" || (oldName == newName && wasDual == isDual) {
		return
	}
	subs, err := h.repo.ListSubscriptions()
	if err != nil {
		log.Printf("subscription rename sync list failed: %v", err)
		return
	}
	var activeNames map[string]bool
	if wasDual && !isDual {
		activeNames, _, _ = h.knownChainEndpointRefs()
	}

	oldV6 := oldName + dualStackV6Suffix
	newV6 := newName + dualStackV6Suffix
	now := time.Now().UnixMilli()

	for i := range subs {
		s := &subs[i]
		pipe := parsePipeline(s.Options)
		changed := false

		replaceList := func(list []string) []string {
			out := make([]string, 0, len(list))
			seen := make(map[string]bool, len(list))
			for _, item := range list {
				next := item
				switch {
				case oldName != newName && item == oldName:
					next = newName
					changed = true
				case item == oldV6:
					if wasDual && !isDual && !activeNames[oldV6] {
						changed = true
						continue
					}
					if wasDual && isDual && oldName != newName {
						next = newV6
						changed = true
					}
				}
				if !seen[next] {
					seen[next] = true
					out = append(out, next)
				} else if next != item {
					changed = true
				}
			}
			return out
		}

		pipe.ChainLandingNames = replaceList(pipe.ChainLandingNames)
		pipe.ChainRelayNames = replaceList(pipe.ChainRelayNames)
		pipe.ChainNoneNames = replaceList(pipe.ChainNoneNames)

		if len(pipe.ChainNodeBindings) > 0 {
			nextBindings := make(map[string]string, len(pipe.ChainNodeBindings))
			for k, v := range pipe.ChainNodeBindings {
				nk, nv := k, v
				drop := false
				if oldName != newName {
					if nk == oldName {
						nk = newName
					} else if wasDual && isDual && nk == oldV6 {
						nk = newV6
					}
					if nv == oldName {
						nv = newName
					} else if wasDual && isDual && nv == oldV6 {
						nv = newV6
					}
				}
				if wasDual && !isDual && !activeNames[oldV6] && (nk == oldV6 || nv == oldV6) {
					drop = true
				}
				if drop {
					changed = true
					continue
				}
				if nk != k || nv != v {
					changed = true
				}
				// Prefer newly renamed entry over stale key collision.
				if _, exists := nextBindings[nk]; !exists || nk != k {
					nextBindings[nk] = nv
				}
			}
			pipe.ChainNodeBindings = nextBindings
		}

		if !changed {
			continue
		}
		raw, err := json.Marshal(pipe)
		if err != nil {
			continue
		}
		s.Options = string(raw)
		s.UpdatedTime = now
		if err := h.repo.SaveSubscription(s); err != nil {
			log.Printf("subscription rename sync save id=%d failed: %v", s.ID, err)
		}
	}
}

// pruneSubscriptionDeletedRefs strips references to deleted endpoints or
// inbound IDs from every subscription while keeping names still served by
// remaining inbounds/relays.
func (h *Handler) pruneSubscriptionDeletedRefs(candidateNames []string, deletedIDs []int64) {
	if len(candidateNames) == 0 && len(deletedIDs) == 0 {
		return
	}
	activeNames, activeIDs, err := h.knownChainEndpointRefs()
	if err != nil {
		log.Printf("subscription delete sync failed: %v", err)
		return
	}
	deadNames := make(map[string]bool)
	for _, name := range candidateNames {
		if name != "" && !activeNames[name] {
			deadNames[name] = true
		}
	}
	deadIDs := make(map[string]bool)
	for _, id := range deletedIDs {
		if id > 0 {
			sID := strconv.FormatInt(id, 10)
			if !activeIDs[sID] {
				deadIDs[sID] = true
			}
		}
	}
	if len(deadNames) == 0 && len(deadIDs) == 0 {
		return
	}

	subs, err := h.repo.ListSubscriptions()
	if err != nil {
		log.Printf("subscription list for prune failed: %v", err)
		return
	}
	now := time.Now().UnixMilli()
	for i := range subs {
		s := &subs[i]
		pipe := parsePipeline(s.Options)
		if summary := applyPrunePipeline(&pipe, deadNames, deadIDs, false); summary == nil {
			continue
		}
		raw, err := json.Marshal(pipe)
		if err != nil {
			continue
		}
		s.Options = string(raw)
		s.UpdatedTime = now
		if err := h.repo.SaveSubscription(s); err != nil {
			log.Printf("subscription prune save id=%d failed: %v", s.ID, err)
		}
	}
}

func applyPrunePipeline(pipe *subPipeline, knownNames, knownIDs map[string]bool, orphanMode bool) *subRefRemovalSummary {
	isDeadName := func(n string) bool {
		if orphanMode {
			return !knownNames[n]
		}
		return knownNames[n]
	}
	isDeadRef := func(k string) bool {
		if orphanMode {
			return !knownNames[k] && !knownIDs[k]
		}
		return knownNames[k] || knownIDs[k]
	}

	summary := &subRefRemovalSummary{Bindings: map[string]string{}}
	filterList := func(in []string, dropped *[]string) []string {
		out := make([]string, 0, len(in))
		for _, n := range in {
			if isDeadName(n) {
				*dropped = append(*dropped, n)
				continue
			}
			out = append(out, n)
		}
		return out
	}

	pipe.ChainLandingNames = filterList(pipe.ChainLandingNames, &summary.LandingNames)
	pipe.ChainRelayNames = filterList(pipe.ChainRelayNames, &summary.RelayNames)
	pipe.ChainNoneNames = filterList(pipe.ChainNoneNames, &summary.NoneNames)

	if len(pipe.ChainNodeBindings) > 0 {
		next := make(map[string]string, len(pipe.ChainNodeBindings))
		for k, v := range pipe.ChainNodeBindings {
			if isDeadRef(k) || isDeadRef(v) {
				summary.Bindings[k] = v
				continue
			}
			next[k] = v
		}
		pipe.ChainNodeBindings = next
	}

	if len(summary.LandingNames) == 0 && len(summary.RelayNames) == 0 && len(summary.NoneNames) == 0 && len(summary.Bindings) == 0 {
		return nil
	}
	if len(summary.Bindings) == 0 {
		summary.Bindings = nil
	}
	return summary
}

func (h *Handler) collectInboundDeletedRefs(rows ...model.NodeInbound) ([]string, []int64) {
	relays, _ := h.repo.ListInboundRelays(0)
	var names []string
	var ids []int64
	byInbound := make(map[int64]bool, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		byInbound[r.ID] = true
		names = append(names, r.Name)
		if inboundShareMode(&r) == "dual" {
			names = append(names, r.Name+dualStackV6Suffix)
		}
	}
	for _, rel := range relays {
		if byInbound[rel.InboundID] {
			names = append(names, rel.Name)
		}
	}
	return names, ids
}

func (h *Handler) subscriptionPruneRefs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		DryRun bool `json:"dryRun"`
	}
	_ = decodeJSON(r.Body, &req)

	activeNames, activeIDs, err := h.knownChainEndpointRefs()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	subs, err := h.repo.ListSubscriptions()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	now := time.Now().UnixMilli()
	removed := make([]subRefRemovalSummary, 0)
	for i := range subs {
		s := &subs[i]
		pipe := parsePipeline(s.Options)
		summary := applyPrunePipeline(&pipe, activeNames, activeIDs, true)
		if summary == nil {
			continue
		}
		summary.SubscriptionID = s.ID
		summary.SubscriptionName = s.Name
		removed = append(removed, *summary)
		if !req.DryRun {
			raw, err := json.Marshal(pipe)
			if err != nil {
				response.WriteJSON(w, response.Err(-2, err.Error()))
				return
			}
			s.Options = string(raw)
			s.UpdatedTime = now
			if err := h.repo.SaveSubscription(s); err != nil {
				response.WriteJSON(w, response.Err(-2, err.Error()))
				return
			}
		}
	}

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"dryRun":               req.DryRun,
		"updatedSubscriptions": len(removed),
		"removed":              removed,
	}))
}
