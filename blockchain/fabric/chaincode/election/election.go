// Package main implements the INEC election-results chaincode (smart contract)
// for Hyperledger Fabric 2.5. It records tamper-evident polling-unit results on
// the ledger and enforces immutability: a result for a polling unit can be
// created once and thereafter only read or superseded by an explicitly
// versioned correction (which preserves the original on-chain).
package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperledger/fabric-contract-api-go/contractapi"
)

// ElectionContract is the smart contract for election result management.
type ElectionContract struct {
	contractapi.Contract
}

// PollingUnitResult is the on-ledger record for a single polling unit.
type PollingUnitResult struct {
	DocType          string         `json:"docType"` // "puResult"
	PollingUnitCode  string         `json:"pollingUnitCode"`
	ElectionID       string         `json:"electionId"`
	AccreditedVoters int            `json:"accreditedVoters"`
	TotalVotes       int            `json:"totalVotes"`
	RejectedVotes    int            `json:"rejectedVotes"`
	PartyVotes       map[string]int `json:"partyVotes"`
	PresidingOfficer string         `json:"presidingOfficer"`
	SubmittedAt      string         `json:"submittedAt"`
	Version          int            `json:"version"`
	PreviousTxID     string         `json:"previousTxId,omitempty"`
}

func resultKey(electionID, puCode string) string {
	return fmt.Sprintf("RESULT_%s_%s", electionID, puCode)
}

// SubmitResult records a polling-unit result. It rejects a second submission
// for the same (election, PU) unless SupersedeResult is used, guaranteeing that
// figures cannot be silently overwritten.
func (c *ElectionContract) SubmitResult(ctx contractapi.TransactionContextInterface,
	resultJSON string) (string, error) {

	var r PollingUnitResult
	if err := json.Unmarshal([]byte(resultJSON), &r); err != nil {
		return "", fmt.Errorf("invalid result payload: %w", err)
	}
	if r.PollingUnitCode == "" || r.ElectionID == "" {
		return "", fmt.Errorf("pollingUnitCode and electionId are required")
	}
	if r.TotalVotes > r.AccreditedVoters {
		return "", fmt.Errorf("total votes (%d) exceed accredited voters (%d): overvoting rejected",
			r.TotalVotes, r.AccreditedVoters)
	}

	key := resultKey(r.ElectionID, r.PollingUnitCode)
	existing, err := ctx.GetStub().GetState(key)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", fmt.Errorf("result for PU %s already exists; use SupersedeResult", r.PollingUnitCode)
	}

	r.DocType = "puResult"
	r.Version = 1
	if r.SubmittedAt == "" {
		r.SubmittedAt = time.Now().UTC().Format(time.RFC3339)
	}
	buf, _ := json.Marshal(r)
	if err := ctx.GetStub().PutState(key, buf); err != nil {
		return "", err
	}
	_ = ctx.GetStub().SetEvent("ResultSubmitted", buf)
	return ctx.GetStub().GetTxID(), nil
}

// SupersedeResult records an audited correction. The prior version stays in the
// block history; the current-state record advances its version and links back.
func (c *ElectionContract) SupersedeResult(ctx contractapi.TransactionContextInterface,
	resultJSON string) (string, error) {

	var r PollingUnitResult
	if err := json.Unmarshal([]byte(resultJSON), &r); err != nil {
		return "", fmt.Errorf("invalid result payload: %w", err)
	}
	key := resultKey(r.ElectionID, r.PollingUnitCode)
	existing, err := ctx.GetStub().GetState(key)
	if err != nil {
		return "", err
	}
	if existing == nil {
		return "", fmt.Errorf("no existing result for PU %s to supersede", r.PollingUnitCode)
	}
	var prev PollingUnitResult
	_ = json.Unmarshal(existing, &prev)

	r.DocType = "puResult"
	r.Version = prev.Version + 1
	r.PreviousTxID = ctx.GetStub().GetTxID()
	r.SubmittedAt = time.Now().UTC().Format(time.RFC3339)
	buf, _ := json.Marshal(r)
	if err := ctx.GetStub().PutState(key, buf); err != nil {
		return "", err
	}
	_ = ctx.GetStub().SetEvent("ResultSuperseded", buf)
	return ctx.GetStub().GetTxID(), nil
}

// GetResult returns the current-state result for a polling unit.
func (c *ElectionContract) GetResult(ctx contractapi.TransactionContextInterface,
	electionID, puCode string) (*PollingUnitResult, error) {

	data, err := ctx.GetStub().GetState(resultKey(electionID, puCode))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("no result for PU %s", puCode)
	}
	var r PollingUnitResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResultHistory returns the full immutable modification history (all versions
// with their transaction IDs and timestamps) for independent audit.
func (c *ElectionContract) GetResultHistory(ctx contractapi.TransactionContextInterface,
	electionID, puCode string) ([]map[string]interface{}, error) {

	iter, err := ctx.GetStub().GetHistoryForKey(resultKey(electionID, puCode))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var history []map[string]interface{}
	for iter.HasNext() {
		rec, err := iter.Next()
		if err != nil {
			return nil, err
		}
		var r PollingUnitResult
		_ = json.Unmarshal(rec.Value, &r)
		history = append(history, map[string]interface{}{
			"txId":      rec.TxId,
			"timestamp": rec.Timestamp.AsTime().Format(time.RFC3339),
			"isDelete":  rec.IsDelete,
			"value":     r,
		})
	}
	return history, nil
}

func main() {
	cc, err := contractapi.NewChaincode(&ElectionContract{})
	if err != nil {
		panic(fmt.Sprintf("create election chaincode: %v", err))
	}
	if err := cc.Start(); err != nil {
		panic(fmt.Sprintf("start election chaincode: %v", err))
	}
}
