package model

type FactValueKind string

const (
	FactValueInteger FactValueKind = "integer"
	FactValueDecimal FactValueKind = "decimal"
	FactValueText    FactValueKind = "text"
	FactValueBoolean FactValueKind = "boolean"
)

type FactValue struct {
	Kind    FactValueKind
	Integer *int64
	Decimal *float64
	Text    *string
	Boolean *bool
}

func NewIntegerValue(value int64) FactValue {
	return FactValue{Kind: FactValueInteger, Integer: &value}
}
func NewDecimalValue(value float64) FactValue {
	return FactValue{Kind: FactValueDecimal, Decimal: &value}
}
func NewTextValue(value string) FactValue  { return FactValue{Kind: FactValueText, Text: &value} }
func NewBooleanValue(value bool) FactValue { return FactValue{Kind: FactValueBoolean, Boolean: &value} }

func (v FactValue) Validate() error {
	set := 0
	for _, present := range []bool{v.Integer != nil, v.Decimal != nil, v.Text != nil, v.Boolean != nil} {
		if present {
			set++
		}
	}
	if set != 1 {
		return invalid("fact_value", "exactly one value field must be set")
	}
	valid := (v.Kind == FactValueInteger && v.Integer != nil) ||
		(v.Kind == FactValueDecimal && v.Decimal != nil) ||
		(v.Kind == FactValueText && v.Text != nil) ||
		(v.Kind == FactValueBoolean && v.Boolean != nil)
	if !valid {
		return invalid("fact_value.kind", "does not match the populated value field")
	}
	return nil
}

type ObservedFact struct {
	ID          string
	SubjectType string
	SubjectID   string
	Key         string
	Value       FactValue
	Unit        string
}

func (f ObservedFact) Validate() error {
	if f.ID == "" {
		return required("fact.id")
	}
	if f.SubjectType == "" {
		return required("fact.subject_type")
	}
	if f.SubjectID == "" {
		return required("fact.subject_id")
	}
	if f.Key == "" {
		return required("fact.key")
	}
	return f.Value.Validate()
}

type EvidenceRef struct {
	ObservationID string
	FactID        string
}

func (r EvidenceRef) Validate() error {
	if r.ObservationID == "" {
		return required("evidence_ref.observation_id")
	}
	if r.FactID == "" {
		return required("evidence_ref.fact_id")
	}
	return nil
}
