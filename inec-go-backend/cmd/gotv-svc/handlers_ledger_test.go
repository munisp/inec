package main

import (
	"testing"
)

func TestHashSHA256Deterministic(t *testing.T) {
	a := hashSHA256("test-data-123")
	b := hashSHA256("test-data-123")
	if a != b {
		t.Errorf("hashSHA256 not deterministic: %s != %s", a, b)
	}
	if len(a) != 64 {
		t.Errorf("hashSHA256 length = %d, want 64", len(a))
	}
}

func TestHashSHA256DifferentInputs(t *testing.T) {
	a := hashSHA256("data-A")
	b := hashSHA256("data-B")
	if a == b {
		t.Error("different inputs should produce different hashes")
	}
}

func TestBuildMerkleTreeSingleLeaf(t *testing.T) {
	tree := buildMerkleTree([]string{"leaf1"})
	if tree == nil {
		t.Fatal("tree should not be nil")
	}
	if len(tree) != 1 {
		t.Errorf("single leaf tree should have 1 level, got %d", len(tree))
	}
	if len(tree[0]) != 1 {
		t.Errorf("level 0 should have 1 node, got %d", len(tree[0]))
	}
}

func TestBuildMerkleTreeTwoLeaves(t *testing.T) {
	tree := buildMerkleTree([]string{"leaf1", "leaf2"})
	if len(tree) != 2 {
		t.Errorf("two leaf tree should have 2 levels, got %d", len(tree))
	}
	// Root should be hash of leaf1_hash + leaf2_hash
	leaf1 := hashSHA256("leaf1")
	leaf2 := hashSHA256("leaf2")
	expectedRoot := hashSHA256(leaf1 + leaf2)
	if tree[1][0] != expectedRoot {
		t.Errorf("root = %s, want %s", tree[1][0], expectedRoot)
	}
}

func TestBuildMerkleTreeFourLeaves(t *testing.T) {
	tree := buildMerkleTree([]string{"a", "b", "c", "d"})
	if len(tree) != 3 {
		t.Errorf("four leaf tree should have 3 levels, got %d", len(tree))
	}
	if len(tree[0]) != 4 {
		t.Errorf("level 0 should have 4 nodes, got %d", len(tree[0]))
	}
	if len(tree[1]) != 2 {
		t.Errorf("level 1 should have 2 nodes, got %d", len(tree[1]))
	}
	if len(tree[2]) != 1 {
		t.Errorf("level 2 should have 1 node (root), got %d", len(tree[2]))
	}
}

func TestBuildMerkleTreeOddLeaves(t *testing.T) {
	tree := buildMerkleTree([]string{"a", "b", "c"})
	if tree == nil || len(tree) < 2 {
		t.Fatal("odd leaf tree should still build")
	}
	// Root should exist
	root := tree[len(tree)-1]
	if len(root) != 1 {
		t.Errorf("root level should have exactly 1 node, got %d", len(root))
	}
}

func TestBuildMerkleTreeEmpty(t *testing.T) {
	tree := buildMerkleTree([]string{})
	if tree != nil {
		t.Error("empty tree should return nil")
	}
}

func TestBuildMerkleTreeDeterministic(t *testing.T) {
	items := []string{"pledge-1", "pledge-2", "pledge-3", "pledge-4"}
	tree1 := buildMerkleTree(items)
	tree2 := buildMerkleTree(items)
	root1 := tree1[len(tree1)-1][0]
	root2 := tree2[len(tree2)-1][0]
	if root1 != root2 {
		t.Errorf("Merkle root not deterministic: %s != %s", root1, root2)
	}
}

func TestGenerateMerkleProof(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	tree := buildMerkleTree(items)
	root := tree[len(tree)-1][0]

	for i := range items {
		proof := generateMerkleProof(tree, i)
		if len(proof) == 0 {
			t.Errorf("proof for leaf %d should not be empty", i)
		}

		// Verify proof
		current := hashSHA256(items[i])
		for _, step := range proof {
			if step.Position == "left" {
				current = hashSHA256(step.Hash + current)
			} else {
				current = hashSHA256(current + step.Hash)
			}
		}
		if current != root {
			t.Errorf("proof verification failed for leaf %d: got %s, want %s", i, current, root)
		}
	}
}

func TestGenerateMerkleProofPositions(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	tree := buildMerkleTree(items)

	// Leaf 0 should need right sibling
	proof0 := generateMerkleProof(tree, 0)
	if proof0[0].Position != "right" {
		t.Errorf("leaf 0 first sibling should be on the right, got %s", proof0[0].Position)
	}

	// Leaf 1 should need left sibling
	proof1 := generateMerkleProof(tree, 1)
	if proof1[0].Position != "left" {
		t.Errorf("leaf 1 first sibling should be on the left, got %s", proof1[0].Position)
	}
}

func TestMerkleTreeLargeInput(t *testing.T) {
	// 100 items
	items := make([]string, 100)
	for i := range items {
		items[i] = hashSHA256("item-" + string(rune('A'+i%26)))
	}
	tree := buildMerkleTree(items)
	if tree == nil {
		t.Fatal("large tree should build")
	}
	root := tree[len(tree)-1]
	if len(root) != 1 {
		t.Errorf("root level should have 1 node, got %d", len(root))
	}
}

func TestTransferCodes(t *testing.T) {
	codes := map[string]int{
		"campaign_spend":     TransferCodeCampaignSpend,
		"ride_cost":          TransferCodeRideCost,
		"volunteer_reimb":    TransferCodeVolunteerReimb,
		"material_purchase":  TransferCodeMaterialPurchase,
		"event_cost":         TransferCodeEventCost,
		"sms_cost":           TransferCodeSMSCost,
		"phone_bank_cost":    TransferCodePhoneBankCost,
	}
	seen := map[int]bool{}
	for name, code := range codes {
		if code <= 0 {
			t.Errorf("transfer code %s should be positive, got %d", name, code)
		}
		if seen[code] {
			t.Errorf("duplicate transfer code: %d for %s", code, name)
		}
		seen[code] = true
	}
}

func TestProofStepJSON(t *testing.T) {
	step := ProofStep{Hash: "abc123", Position: "left"}
	if step.Hash != "abc123" {
		t.Errorf("hash = %s, want abc123", step.Hash)
	}
	if step.Position != "left" {
		t.Errorf("position = %s, want left", step.Position)
	}
}

func TestContainsHelper(t *testing.T) {
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"hello world", "world", true},
		{"hello world", "xyz", false},
		{"blockchain", "chain", true},
		{"", "test", false},
		{"test", "", true},
	}
	for _, tt := range tests {
		got := containsHelper(tt.s, tt.sub)
		if got != tt.want {
			t.Errorf("containsHelper(%q, %q) = %v, want %v", tt.s, tt.sub, got, tt.want)
		}
	}
}
