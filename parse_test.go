package main

import "testing"

func TestScenarios(t *testing.T) {
	list, errs := ListScenarios("scenarios")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(list) < 5 {
		t.Fatalf("want >=5 scenarios, got %d", len(list))
	}
	for _, s := range list {
		sum := 0
		for _, k := range s.Tasks {
			sum += k.Weight
			if k.Verify == "" || k.Solution == "" || k.Body == "" {
				t.Errorf("%s task %d incomplete", s.ID, k.Number)
			}
		}
		if sum != 100 {
			t.Errorf("%s weights sum to %d", s.ID, sum)
		}
		if s.Setup == "" || s.Cleanup == "" {
			t.Errorf("%s missing setup/cleanup", s.ID)
		}
	}
}
