// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	strfmt "github.com/go-openapi/strfmt"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/swag"
)

// EndpointIPIdentityMapping mapping of endpoint IP to security identity
// swagger:model EndpointIPIdentityMapping

type EndpointIPIdentityMapping struct {

	// security identity
	ID int64 `json:"id,omitempty"`

	// endpoint ip
	IP string `json:"ip,omitempty"`
}

/* polymorph EndpointIPIdentityMapping id false */

/* polymorph EndpointIPIdentityMapping ip false */

// Validate validates this endpoint IP identity mapping
func (m *EndpointIPIdentityMapping) Validate(formats strfmt.Registry) error {
	var res []error

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

// MarshalBinary interface implementation
func (m *EndpointIPIdentityMapping) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *EndpointIPIdentityMapping) UnmarshalBinary(b []byte) error {
	var res EndpointIPIdentityMapping
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
