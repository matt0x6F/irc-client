package sasl

import "fmt"

func NewScram(mech, username, password string) (Mechanism, error) {
	return nil, fmt.Errorf("scram not yet implemented")
}
