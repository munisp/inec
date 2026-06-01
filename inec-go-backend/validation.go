package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

func initValidator() {
	validate = validator.New(validator.WithRequiredStructEnabled())
}

// ResultSubmission is the validated input for submitting election results.
type ResultSubmission struct {
	ElectionID      int    `json:"election_id" validate:"required,gt=0"`
	PollingUnitCode string `json:"polling_unit_code" validate:"required,min=3"`
	PartyCode       string `json:"party_code" validate:"required,min=2"`
	Votes           int    `json:"votes" validate:"gte=0"`
	IdempotencyKey  string `json:"idempotency_key" validate:"required,uuid"`
}

// IncidentReport is the validated input for reporting an incident.
type IncidentReport struct {
	ElectionID      int    `json:"election_id" validate:"gt=0"`
	PollingUnitCode string `json:"polling_unit_code"`
	IncidentType    string `json:"incident_type" validate:"required,min=3,max=100"`
	Description     string `json:"description" validate:"required,min=5,max=2000"`
	Severity        string `json:"severity" validate:"oneof=low medium high critical"`
	StateCode       string `json:"state_code"`
}

// CollationRequest is the validated input for collation operations.
type CollationRequest struct {
	ElectionID int    `json:"election_id" validate:"required,gt=0"`
	Level      string `json:"level" validate:"required,oneof=ward lga state national"`
	Code       string `json:"code" validate:"required"`
}

// BVASRegistration is the validated input for registering a BVAS device.
type BVASRegistration struct {
	DeviceID        string  `json:"device_id"`
	SerialNumber    string  `json:"serial_number" validate:"required"`
	PollingUnitCode string  `json:"polling_unit_code"`
	ElectionID      int     `json:"election_id"`
	StateCode       string  `json:"state_code"`
	FirmwareVersion string  `json:"firmware_version"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
}

// VoterRegistration is the validated input for voter registration.
type VoterRegistration struct {
	VIN         string `json:"vin" validate:"required,len=19"`
	FirstName   string `json:"first_name" validate:"required,min=2"`
	LastName    string `json:"last_name" validate:"required,min=2"`
	DateOfBirth string `json:"date_of_birth" validate:"required"`
	Gender      string `json:"gender" validate:"required,oneof=M F"`
	StateCode   string `json:"state_code" validate:"required"`
	LGACode     string `json:"lga_code" validate:"required"`
	WardCode    string `json:"ward_code" validate:"required"`
	PUCode      string `json:"pu_code" validate:"required"`
}

// ElectionCreate is the validated input for creating an election.
type ElectionCreate struct {
	Title        string `json:"title" validate:"required,min=3,max=200"`
	ElectionType string `json:"election_type" validate:"required,oneof=presidential gubernatorial senatorial house_of_reps"`
	ElectionDate string `json:"election_date" validate:"required"`
	Description  string `json:"description" validate:"max=2000"`
	Status       string `json:"status" validate:"omitempty,oneof=upcoming active completed cancelled"`
}

// UserPromotion is the validated input for promoting a user to a new role.
type UserPromotion struct {
	UserID int    `json:"user_id" validate:"required,gt=0"`
	Role   string `json:"role" validate:"required,oneof=admin presiding_officer collation_officer observer public"`
}

// decodeAndValidate decodes JSON body into dest and validates it.
func decodeAndValidate(r *http.Request, dest interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := validate.Struct(dest); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			msgs := make([]string, 0, len(validationErrors))
			for _, ve := range validationErrors {
				msgs = append(msgs, fmt.Sprintf("field '%s' failed on '%s'", ve.Field(), ve.Tag()))
			}
			return fmt.Errorf("validation failed: %v", msgs)
		}
		return err
	}
	return nil
}
