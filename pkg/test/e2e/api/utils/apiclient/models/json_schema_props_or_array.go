// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"strconv"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
)

// JSONSchemaPropsOrArray JSONSchemaPropsOrArray represents a value that can either be a JSONSchemaProps
// or an array of JSONSchemaProps. Mainly here for serialization purposes.
//
// swagger:model JSONSchemaPropsOrArray
type JSONSchemaPropsOrArray struct {

	// JSON schemas
	JSONSchemas []*JSONSchemaProps `json:"JSONSchemas"`

	// schema
	Schema *JSONSchemaProps `json:"Schema,omitempty"`
}

// Validate validates this JSON schema props or array
func (m *JSONSchemaPropsOrArray) Validate(formats strfmt.Registry) error {
	var res []error

	if err := m.validateJSONSchemas(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateSchema(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *JSONSchemaPropsOrArray) validateJSONSchemas(formats strfmt.Registry) error {

	if swag.IsZero(m.JSONSchemas) { // not required
		return nil
	}

	for i := 0; i < len(m.JSONSchemas); i++ {
		if swag.IsZero(m.JSONSchemas[i]) { // not required
			continue
		}

		if m.JSONSchemas[i] != nil {
			if err := m.JSONSchemas[i].Validate(formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("JSONSchemas" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

func (m *JSONSchemaPropsOrArray) validateSchema(formats strfmt.Registry) error {

	if swag.IsZero(m.Schema) { // not required
		return nil
	}

	if m.Schema != nil {
		if err := m.Schema.Validate(formats); err != nil {
			if ve, ok := err.(*errors.Validation); ok {
				return ve.ValidateName("Schema")
			}
			return err
		}
	}

	return nil
}

// MarshalBinary interface implementation
func (m *JSONSchemaPropsOrArray) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *JSONSchemaPropsOrArray) UnmarshalBinary(b []byte) error {
	var res JSONSchemaPropsOrArray
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}