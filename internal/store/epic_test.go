package store

import (
	"strings"
	"testing"
)

func TestSetPrefixedLabelKeepsOtherPrefixes(t *testing.T) {
	db := openTest(t)
	task := mkTask(t, db, StatusBacklog)
	for _, l := range []string{"epic:os", "epic_kind:build", "epic_stage:chart", "plain"} {
		if err := AddLabel(db, task.ID, l); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetPrefixedLabel(db, task.ID, "epic_stage:spec"); err != nil {
		t.Fatal(err)
	}
	got, _ := GetTask(db, task.ID)
	if v := LabelValue(got, PrefixStage); v != "spec" {
		t.Fatalf("stage = %q", v)
	}
	if EpicName(got) != "os" || LabelValue(got, PrefixKind) != "build" || !hasLabel(got.Labels, "plain") {
		t.Fatalf("other labels disturbed: %v", got.Labels)
	}
	if err := SetPrefixedLabel(db, task.ID, "nocolon"); err == nil {
		t.Fatal("want error for label without prefix:value")
	}
}

func TestEpicNameUniqueAmongNotDone(t *testing.T) {
	db := openTest(t)
	a, b := mkTask(t, db, StatusBacklog), mkTask(t, db, StatusBacklog)
	if err := MakeEpic(db, a.ID, "os"); err != nil {
		t.Fatal(err)
	}
	if err := MakeEpic(db, b.ID, "OS"); err == nil || !strings.Contains(err.Error(), "taken") {
		t.Fatalf("want name-taken error, got %v", err)
	}
	if _, err := UpdateTaskStatus(db, a.ID, StatusCancelled, ActorHuman); err != nil {
		t.Fatal(err)
	}
	if err := MakeEpic(db, b.ID, "os"); err != nil {
		t.Fatalf("cancelled epic still holds name: %v", err)
	}
}

func TestOneActiveFlowAndRestart(t *testing.T) {
	db := openTest(t)
	e := mkTask(t, db, StatusBacklog)
	if _, err := StartFlow(db, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := StartFlow(db, e.ID); err == nil {
		t.Fatal("second active flow must fail")
	}
	f, err := RestartFlow(db, e.ID, "wrong plan")
	if err != nil || f.Attempt != 2 {
		t.Fatalf("restart: %v %+v", err, f)
	}
	flows, _ := ListFlows(db, e.ID)
	if len(flows) != 2 || flows[0].Status != FlowAbandoned || flows[0].Reason != "wrong plan" || flows[1].Status != FlowActive {
		t.Fatalf("flows = %+v", flows)
	}
	got, _ := GetTask(db, e.ID)
	if EpicStage(got) != StageSpec {
		t.Fatalf("stage = %s", EpicStage(got))
	}
}
