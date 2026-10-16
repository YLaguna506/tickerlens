package pipeline

import (
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/agentclient"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

func TestChooseSetsLanguageAndModel(t *testing.T) {
	j := &job{opt: store.Options{Lang: "es", Model: "openrouter:nvidia/nemotron-3-super-120b-a12b:free"}}
	got := j.choose(agentclient.Task{Section: AgentOverview})
	if got.Language != "es" || got.Provider != "openrouter" || got.Model != "nvidia/nemotron-3-super-120b-a12b:free" {
		t.Errorf("task = %+v", got)
	}

	// The defaults leave the task untouched, so English caches keep working.
	plain := (&job{}).choose(agentclient.Task{Section: AgentOverview})
	if plain.Language != "" || plain.Provider != "" || plain.Model != "" {
		t.Errorf("default task = %+v", plain)
	}
	if langSuffix(store.Options{}) != "" || langSuffix(store.Options{Lang: "es"}) != ":es" {
		t.Error("only non-English keys get a language suffix")
	}
	if modelOr(store.Options{}, "m") != "m" || modelOr(store.Options{Model: "p:x"}, "m") != "p:x" {
		t.Error("modelOr")
	}
}
