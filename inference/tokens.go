// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inference

import (
	"fmt"
	"unicode/utf8"
)

const (
	characterUnitsPerToken = 4
	nonASCIICharacterUnits = 4
)

// TokenEstimatorProfile is the stable deployment identity of a token policy.
type TokenEstimatorProfile struct {
	estimatorID string
	version     string
}

func NewTokenEstimatorProfile(estimatorID, version string) (TokenEstimatorProfile, error) {
	if err := validateEstimatorText("estimator_id", estimatorID, 128); err != nil {
		return TokenEstimatorProfile{}, err
	}
	if err := validateEstimatorText("version", version, 64); err != nil {
		return TokenEstimatorProfile{}, err
	}
	return TokenEstimatorProfile{estimatorID: estimatorID, version: version}, nil
}

func (p TokenEstimatorProfile) EstimatorID() string { return p.estimatorID }
func (p TokenEstimatorProfile) Version() string     { return p.version }

// TokenEstimator validates the result of a deployment-fixed token counter.
type TokenEstimator struct {
	profile TokenEstimatorProfile
	count   func(string) int
}

func NewTokenEstimator(profile TokenEstimatorProfile, count func(string) int) (*TokenEstimator, error) {
	if _, err := NewTokenEstimatorProfile(profile.estimatorID, profile.version); err != nil {
		return nil, err
	}
	if count == nil {
		return nil, fmt.Errorf("token estimator count function must not be nil")
	}
	return &TokenEstimator{profile: profile, count: count}, nil
}

func (e *TokenEstimator) Profile() TokenEstimatorProfile { return e.profile }

func (e *TokenEstimator) Estimate(text string) (int, error) {
	value := e.count(text)
	if value < 0 {
		return 0, fmt.Errorf("token estimator must return a non-negative integer")
	}
	return value, nil
}

func CharacterTokenEstimator() *TokenEstimator {
	profile, _ := NewTokenEstimatorProfile("character:weighted", "1")
	estimator, _ := NewTokenEstimator(profile, func(text string) int {
		units := 0
		for _, character := range text {
			if character <= 0x7f {
				units++
			} else {
				units += nonASCIICharacterUnits
			}
		}
		return (units + characterUnitsPerToken - 1) / characterUnitsPerToken
	})
	return estimator
}

func validateEstimatorText(field, value string, maximum int) error {
	if value == "" {
		return fmt.Errorf("token estimator %s must not be empty", field)
	}
	if utf8.RuneCountInString(value) > maximum {
		return fmt.Errorf("token estimator %s must not exceed %d characters", field, maximum)
	}
	return nil
}
