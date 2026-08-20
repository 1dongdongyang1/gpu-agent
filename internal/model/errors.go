package model

import "fmt"

func required(field string) error {
	return fmt.Errorf("%s is required", field)
}

func invalid(field, reason string) error {
	return fmt.Errorf("invalid %s: %s", field, reason)
}
