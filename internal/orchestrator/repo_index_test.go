package orchestrator

import "testing"

func TestInferIndexedKindsClassifiesPageComponentsWithoutLosingTestSignal(t *testing.T) {
	kinds := inferIndexedKinds("web/src/pages/Landing.tsx", "export default function Landing() { return null }\n")
	if !containsFold(kinds, "route") {
		t.Fatalf("expected route kind, got %v", kinds)
	}
	if !containsFold(kinds, "component") {
		t.Fatalf("expected component kind, got %v", kinds)
	}

	testKinds := inferIndexedKinds("web/src/pages/Landing.test.tsx", "describe('Landing', () => {})\n")
	if containsFold(testKinds, "component") {
		t.Fatalf("expected test file not to be classified as component, got %v", testKinds)
	}
	if !containsFold(testKinds, "test") {
		t.Fatalf("expected test kind, got %v", testKinds)
	}
}
