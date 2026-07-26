package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hyperledger/fabric-contract-api-go/contractapi"
)

const (
	anchorSchemaVersion     = 1
	evidenceAnchorChaincode = "evidence-anchor"
	governanceStateKey      = "governance:v1"
	anchorStatePrefix       = "anchor:"
)

// EvidenceAnchorContract stores only a signed, non-sensitive commitment to an
// off-chain election-evidence event or collation bundle. Raw documents, voter
// data, and private reconciliation detail must never be passed to this contract.
type EvidenceAnchorContract struct {
	contractapi.Contract
}

type EvidenceAnchorV1 struct {
	SchemaVersion   int    `json:"schema_version"`
	AnchorID        string `json:"anchor_id"`
	AnchorType      string `json:"anchor_type"`
	ElectionID      int64  `json:"election_id"`
	ResultID        int64  `json:"result_id,omitempty"`
	EventHash       string `json:"event_hash"`
	PayloadSHA256   string `json:"payload_sha256"`
	PriorEventHash  string `json:"prior_event_hash,omitempty"`
	Signature       string `json:"signature"`
	SignerKeyID     string `json:"signer_key_id"`
	PolicyVersionID int64  `json:"policy_version_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	CreatorMSP      string `json:"creator_msp"`
	FabricTxID      string `json:"fabric_tx_id,omitempty"`
}

type EvidenceAnchorReceipt struct {
	AnchorID      string `json:"anchor_id"`
	FabricTxID    string `json:"fabric_tx_id"`
	ChannelID     string `json:"channel_id"`
	ChaincodeName string `json:"chaincode_name"`
	CreatorMSP    string `json:"creator_msp"`
	CreatedAt     string `json:"created_at"`
	Idempotent    bool   `json:"idempotent"`
}

type FabricGovernanceV1 struct {
	SchemaVersion  int      `json:"schema_version"`
	ConsortiumMSPs []string `json:"consortium_msps"`
	CreatedByMSP   string   `json:"created_by_msp"`
	CreatedAt      string   `json:"created_at"`
}

type anchorIDInput struct {
	SchemaVersion   int    `json:"schema_version"`
	AnchorType      string `json:"anchor_type"`
	ElectionID      int64  `json:"election_id"`
	ResultID        int64  `json:"result_id,omitempty"`
	EventHash       string `json:"event_hash"`
	PayloadSHA256   string `json:"payload_sha256"`
	PriorEventHash  string `json:"prior_event_hash,omitempty"`
	Signature       string `json:"signature"`
	SignerKeyID     string `json:"signer_key_id"`
	PolicyVersionID int64  `json:"policy_version_id,omitempty"`
	CreatedAt       string `json:"created_at"`
}

func main() {
	chaincode, err := contractapi.NewChaincode(&EvidenceAnchorContract{})
	if err != nil {
		panic(fmt.Errorf("create evidence-anchor chaincode: %w", err))
	}
	if err := chaincode.Start(); err != nil {
		panic(fmt.Errorf("start evidence-anchor chaincode: %w", err))
	}
}

// InitializeGovernance stores the explicit MSP membership for anchor creators.
// It is one-time only; changing consortium membership requires a new governed
// chaincode definition and a documented migration rather than silent mutation.
func (c *EvidenceAnchorContract) InitializeGovernance(ctx contractapi.TransactionContextInterface, consortiumMSPsJSON string) (*FabricGovernanceV1, error) {
	existing, err := ctx.GetStub().GetState(governanceStateKey)
	if err != nil {
		return nil, fmt.Errorf("read Fabric governance: %w", err)
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("Fabric anchor governance is already initialized")
	}

	var MSPs []string
	if err := json.Unmarshal([]byte(consortiumMSPsJSON), &MSPs); err != nil {
		return nil, fmt.Errorf("decode consortium MSP list: %w", err)
	}
	MSPs, err = normalizeMSPs(MSPs)
	if err != nil {
		return nil, err
	}
	if len(MSPs) < 2 {
		return nil, fmt.Errorf("at least two consortium MSPs are required")
	}

	callerMSP, err := ctx.GetClientIdentity().GetMSPID()
	if err != nil {
		return nil, fmt.Errorf("resolve Fabric caller MSP: %w", err)
	}
	if !containsMSP(MSPs, callerMSP) {
		return nil, fmt.Errorf("caller MSP %q is not included in proposed governance", callerMSP)
	}

	governance := &FabricGovernanceV1{
		SchemaVersion:  anchorSchemaVersion,
		ConsortiumMSPs: MSPs,
		CreatedByMSP:   callerMSP,
		CreatedAt:      fabricTimestamp(ctx),
	}
	encoded, err := json.Marshal(governance)
	if err != nil {
		return nil, fmt.Errorf("encode Fabric governance: %w", err)
	}
	if err := ctx.GetStub().PutState(governanceStateKey, encoded); err != nil {
		return nil, fmt.Errorf("persist Fabric governance: %w", err)
	}
	return governance, nil
}

func (c *EvidenceAnchorContract) ReadGovernance(ctx contractapi.TransactionContextInterface) (*FabricGovernanceV1, error) {
	governance, err := readGovernance(ctx)
	if err != nil {
		return nil, err
	}
	return governance, nil
}

// CreateAnchor writes one deterministic commitment. The chaincode-level
// endorsement policy is deployed as an explicit consortium policy; the contract
// additionally limits clients to the approved governance MSPs.
func (c *EvidenceAnchorContract) CreateAnchor(ctx contractapi.TransactionContextInterface, anchorJSON string) (*EvidenceAnchorReceipt, error) {
	governance, err := readGovernance(ctx)
	if err != nil {
		return nil, err
	}
	callerMSP, err := ctx.GetClientIdentity().GetMSPID()
	if err != nil {
		return nil, fmt.Errorf("resolve Fabric caller MSP: %w", err)
	}
	if !containsMSP(governance.ConsortiumMSPs, callerMSP) {
		return nil, fmt.Errorf("caller MSP %q is not authorized to create evidence anchors", callerMSP)
	}

	var anchor EvidenceAnchorV1
	if err := json.Unmarshal([]byte(anchorJSON), &anchor); err != nil {
		return nil, fmt.Errorf("decode evidence anchor: %w", err)
	}
	if err := validateAnchor(&anchor); err != nil {
		return nil, err
	}
	computedID, err := deterministicAnchorID(anchor)
	if err != nil {
		return nil, err
	}
	if anchor.AnchorID != computedID {
		return nil, fmt.Errorf("anchor_id does not match canonical anchor commitment")
	}

	key := anchorStatePrefix + anchor.AnchorID
	existing, err := ctx.GetStub().GetState(key)
	if err != nil {
		return nil, fmt.Errorf("read existing Fabric anchor: %w", err)
	}
	if len(existing) > 0 {
		var stored EvidenceAnchorV1
		if err := json.Unmarshal(existing, &stored); err != nil {
			return nil, fmt.Errorf("decode stored Fabric anchor: %w", err)
		}
		storedID, err := deterministicAnchorID(stored)
		if err != nil || storedID != anchor.AnchorID {
			return nil, fmt.Errorf("conflicting existing Fabric anchor state")
		}
		return &EvidenceAnchorReceipt{
			AnchorID:      anchor.AnchorID,
			FabricTxID:    stored.FabricTxID,
			ChannelID:     ctx.GetStub().GetChannelID(),
			ChaincodeName: evidenceAnchorChaincode,
			CreatorMSP:    stored.CreatorMSP,
			CreatedAt:     stored.CreatedAt,
			Idempotent:    true,
		}, nil
	}

	anchor.CreatorMSP = callerMSP
	anchor.FabricTxID = ctx.GetStub().GetTxID()
	encoded, err := json.Marshal(anchor)
	if err != nil {
		return nil, fmt.Errorf("encode Fabric anchor: %w", err)
	}
	if err := ctx.GetStub().PutState(key, encoded); err != nil {
		return nil, fmt.Errorf("persist Fabric anchor: %w", err)
	}
	if err := ctx.GetStub().SetEvent("EvidenceAnchorCreated", encoded); err != nil {
		return nil, fmt.Errorf("emit Fabric anchor event: %w", err)
	}
	return &EvidenceAnchorReceipt{
		AnchorID:      anchor.AnchorID,
		FabricTxID:    anchor.FabricTxID,
		ChannelID:     ctx.GetStub().GetChannelID(),
		ChaincodeName: evidenceAnchorChaincode,
		CreatorMSP:    callerMSP,
		CreatedAt:     anchor.CreatedAt,
		Idempotent:    false,
	}, nil
}

func (c *EvidenceAnchorContract) ReadAnchor(ctx contractapi.TransactionContextInterface, anchorID string) (*EvidenceAnchorV1, error) {
	if !isSHA256Hex(anchorID) {
		return nil, fmt.Errorf("anchor_id must be a SHA-256 hex value")
	}
	value, err := ctx.GetStub().GetState(anchorStatePrefix + anchorID)
	if err != nil {
		return nil, fmt.Errorf("read Fabric anchor: %w", err)
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("Fabric anchor %q does not exist", anchorID)
	}
	var anchor EvidenceAnchorV1
	if err := json.Unmarshal(value, &anchor); err != nil {
		return nil, fmt.Errorf("decode Fabric anchor: %w", err)
	}
	return &anchor, nil
}

func (c *EvidenceAnchorContract) AnchorExists(ctx contractapi.TransactionContextInterface, anchorID string) (bool, error) {
	if !isSHA256Hex(anchorID) {
		return false, fmt.Errorf("anchor_id must be a SHA-256 hex value")
	}
	value, err := ctx.GetStub().GetState(anchorStatePrefix + anchorID)
	if err != nil {
		return false, fmt.Errorf("read Fabric anchor: %w", err)
	}
	return len(value) > 0, nil
}

func (c *EvidenceAnchorContract) GetAnchorHistory(ctx contractapi.TransactionContextInterface, anchorID string) ([]EvidenceAnchorV1, error) {
	if !isSHA256Hex(anchorID) {
		return nil, fmt.Errorf("anchor_id must be a SHA-256 hex value")
	}
	iterator, err := ctx.GetStub().GetHistoryForKey(anchorStatePrefix + anchorID)
	if err != nil {
		return nil, fmt.Errorf("read Fabric anchor history: %w", err)
	}
	defer iterator.Close()

	history := make([]EvidenceAnchorV1, 0)
	for iterator.HasNext() {
		entry, err := iterator.Next()
		if err != nil {
			return nil, fmt.Errorf("iterate Fabric anchor history: %w", err)
		}
		if entry.IsDelete || len(entry.Value) == 0 {
			return nil, fmt.Errorf("Fabric anchor history contains a prohibited deletion")
		}
		var anchor EvidenceAnchorV1
		if err := json.Unmarshal(entry.Value, &anchor); err != nil {
			return nil, fmt.Errorf("decode Fabric anchor history: %w", err)
		}
		history = append(history, anchor)
	}
	return history, nil
}

func readGovernance(ctx contractapi.TransactionContextInterface) (*FabricGovernanceV1, error) {
	value, err := ctx.GetStub().GetState(governanceStateKey)
	if err != nil {
		return nil, fmt.Errorf("read Fabric governance: %w", err)
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("Fabric anchor governance is not initialized")
	}
	var governance FabricGovernanceV1
	if err := json.Unmarshal(value, &governance); err != nil {
		return nil, fmt.Errorf("decode Fabric governance: %w", err)
	}
	MSPs, err := normalizeMSPs(governance.ConsortiumMSPs)
	if err != nil || len(MSPs) < 2 {
		return nil, fmt.Errorf("Fabric anchor governance is invalid")
	}
	governance.ConsortiumMSPs = MSPs
	return &governance, nil
}

func validateAnchor(anchor *EvidenceAnchorV1) error {
	if anchor.SchemaVersion != anchorSchemaVersion {
		return fmt.Errorf("unsupported evidence anchor schema version")
	}
	if anchor.AnchorType != "result_event" && anchor.AnchorType != "collation_bundle" {
		return fmt.Errorf("unsupported evidence anchor type")
	}
	if anchor.ElectionID <= 0 {
		return fmt.Errorf("election_id is required")
	}
	if anchor.AnchorType == "result_event" && anchor.ResultID <= 0 {
		return fmt.Errorf("result_id is required for result_event anchors")
	}
	for field, value := range map[string]string{
		"anchor_id": anchor.AnchorID, "event_hash": anchor.EventHash, "payload_sha256": anchor.PayloadSHA256,
	} {
		if !isSHA256Hex(value) {
			return fmt.Errorf("%s must be a SHA-256 hex value", field)
		}
	}
	if anchor.PriorEventHash != "" && !isSHA256Hex(anchor.PriorEventHash) {
		return fmt.Errorf("prior_event_hash must be a SHA-256 hex value when provided")
	}
	if strings.TrimSpace(anchor.Signature) == "" || strings.TrimSpace(anchor.SignerKeyID) == "" {
		return fmt.Errorf("a signed anchor requires signature and signer_key_id")
	}
	if _, err := base64.StdEncoding.DecodeString(anchor.Signature); err != nil {
		return fmt.Errorf("signature must be standard base64: %w", err)
	}
	if anchor.PolicyVersionID < 0 {
		return fmt.Errorf("policy_version_id cannot be negative")
	}
	if _, err := time.Parse(time.RFC3339, anchor.CreatedAt); err != nil {
		return fmt.Errorf("created_at must be an RFC3339 timestamp: %w", err)
	}
	return nil
}

func deterministicAnchorID(anchor EvidenceAnchorV1) (string, error) {
	canonical, err := json.Marshal(anchorIDInput{
		SchemaVersion:   anchor.SchemaVersion,
		AnchorType:      anchor.AnchorType,
		ElectionID:      anchor.ElectionID,
		ResultID:        anchor.ResultID,
		EventHash:       anchor.EventHash,
		PayloadSHA256:   anchor.PayloadSHA256,
		PriorEventHash:  anchor.PriorEventHash,
		Signature:       anchor.Signature,
		SignerKeyID:     anchor.SignerKeyID,
		PolicyVersionID: anchor.PolicyVersionID,
		CreatedAt:       anchor.CreatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize Fabric anchor: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func normalizeMSPs(values []string) ([]string, error) {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("consortium MSP IDs cannot be empty")
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func containsMSP(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func fabricTimestamp(ctx contractapi.TransactionContextInterface) string {
	timestamp, err := ctx.GetStub().GetTxTimestamp()
	if err != nil || timestamp == nil {
		return time.Now().UTC().Format(time.RFC3339)
	}
	return time.Unix(timestamp.Seconds, int64(timestamp.Nanos)).UTC().Format(time.RFC3339)
}
