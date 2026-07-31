package daily

import (
	"errors"
	"testing"
)

func TestRunStepRecordsFailureAndReportsProgress(t *testing.T) {
	var steps []Step
	var updates []Step
	err := runStep(&steps, func(step Step) { updates = append(updates, step) }, "测试步骤", func() (string, error) {
		return "", errors.New("网络不可用")
	})
	if err == nil {
		t.Fatal("runStep() error = nil")
	}
	if len(steps) != 1 || steps[0].State != StepFailed || steps[0].Detail != "网络不可用" {
		t.Fatalf("steps = %+v", steps)
	}
	if len(updates) != 1 || updates[0].Name != "测试步骤" {
		t.Fatalf("updates = %+v", updates)
	}
}
