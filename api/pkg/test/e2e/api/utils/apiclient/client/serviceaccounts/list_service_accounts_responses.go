// Code generated by go-swagger; DO NOT EDIT.

package serviceaccounts

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"

	"github.com/go-openapi/runtime"

	strfmt "github.com/go-openapi/strfmt"

	models "github.com/kubermatic/kubermatic/api/pkg/test/e2e/api/utils/apiclient/models"
)

// ListServiceAccountsReader is a Reader for the ListServiceAccounts structure.
type ListServiceAccountsReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *ListServiceAccountsReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {

	case 200:
		result := NewListServiceAccountsOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil

	case 401:
		result := NewListServiceAccountsUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result

	case 403:
		result := NewListServiceAccountsForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result

	default:
		result := NewListServiceAccountsDefault(response.Code())
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		if response.Code()/100 == 2 {
			return result, nil
		}
		return nil, result
	}
}

// NewListServiceAccountsOK creates a ListServiceAccountsOK with default headers values
func NewListServiceAccountsOK() *ListServiceAccountsOK {
	return &ListServiceAccountsOK{}
}

/*ListServiceAccountsOK handles this case with default header values.

ServiceAccount
*/
type ListServiceAccountsOK struct {
	Payload []*models.ServiceAccount
}

func (o *ListServiceAccountsOK) Error() string {
	return fmt.Sprintf("[GET /api/v1/projects/{project_id}/serviceaccounts][%d] listServiceAccountsOK  %+v", 200, o.Payload)
}

func (o *ListServiceAccountsOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	// response payload
	if err := consumer.Consume(response.Body(), &o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewListServiceAccountsUnauthorized creates a ListServiceAccountsUnauthorized with default headers values
func NewListServiceAccountsUnauthorized() *ListServiceAccountsUnauthorized {
	return &ListServiceAccountsUnauthorized{}
}

/*ListServiceAccountsUnauthorized handles this case with default header values.

EmptyResponse is a empty response
*/
type ListServiceAccountsUnauthorized struct {
}

func (o *ListServiceAccountsUnauthorized) Error() string {
	return fmt.Sprintf("[GET /api/v1/projects/{project_id}/serviceaccounts][%d] listServiceAccountsUnauthorized ", 401)
}

func (o *ListServiceAccountsUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewListServiceAccountsForbidden creates a ListServiceAccountsForbidden with default headers values
func NewListServiceAccountsForbidden() *ListServiceAccountsForbidden {
	return &ListServiceAccountsForbidden{}
}

/*ListServiceAccountsForbidden handles this case with default header values.

EmptyResponse is a empty response
*/
type ListServiceAccountsForbidden struct {
}

func (o *ListServiceAccountsForbidden) Error() string {
	return fmt.Sprintf("[GET /api/v1/projects/{project_id}/serviceaccounts][%d] listServiceAccountsForbidden ", 403)
}

func (o *ListServiceAccountsForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewListServiceAccountsDefault creates a ListServiceAccountsDefault with default headers values
func NewListServiceAccountsDefault(code int) *ListServiceAccountsDefault {
	return &ListServiceAccountsDefault{
		_statusCode: code,
	}
}

/*ListServiceAccountsDefault handles this case with default header values.

ErrorResponse is the default representation of an error
*/
type ListServiceAccountsDefault struct {
	_statusCode int

	Payload *models.ErrorDetails
}

// Code gets the status code for the list service accounts default response
func (o *ListServiceAccountsDefault) Code() int {
	return o._statusCode
}

func (o *ListServiceAccountsDefault) Error() string {
	return fmt.Sprintf("[GET /api/v1/projects/{project_id}/serviceaccounts][%d] listServiceAccounts default  %+v", o._statusCode, o.Payload)
}

func (o *ListServiceAccountsDefault) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.ErrorDetails)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}