// Copyright 2025 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package dcr

import (
	"strconv"

	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
)

// errInvalidBCP47Tag is returned when a language tag in a DCR request field is not valid BCP 47.
type errInvalidBCP47Tag struct{ key string }

// Error implements the error interface.
func (e *errInvalidBCP47Tag) Error() string {
	return "invalid BCP 47 language tag in field \"" + e.key + "\""
}

// errTooManyLocalizedVariants is returned when a localizable field exceeds maxLocalizedVariantsPerField.
type errTooManyLocalizedVariants struct{ field string }

// Error implements the error interface.
func (e *errTooManyLocalizedVariants) Error() string {
	return "field \"" + e.field + "\" exceeds the maximum of " +
		strconv.Itoa(maxLocalizedVariantsPerField) + " localized variants"
}

// DCR standard service error constants
var (
	// ErrorInvalidRequestFormat is used for nil request validation
	ErrorInvalidRequestFormat = tidcommon.ServiceError{
		Type: tidcommon.ClientErrorType,
		Code: "invalid_client_metadata",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.invalid_request_format",
			DefaultValue: "Invalid request format",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.invalid_request_format_description",
			DefaultValue: "The request body is missing or has an invalid format",
		},
	}

	// ErrorInvalidRedirectURI is the standard error for redirect URI issues
	ErrorInvalidRedirectURI = tidcommon.ServiceError{
		Type: tidcommon.ClientErrorType,
		Code: "invalid_redirect_uri",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.invalid_redirect_uri",
			DefaultValue: "Invalid redirect URI",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.invalid_redirect_uri_description",
			DefaultValue: "One or more redirect URIs are invalid",
		},
	}

	// ErrorInvalidClientMetadata is the standard error for client metadata issues
	ErrorInvalidClientMetadata = tidcommon.ServiceError{
		Type: tidcommon.ClientErrorType,
		Code: "invalid_client_metadata",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.invalid_client_metadata",
			DefaultValue: "Invalid client metadata",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.invalid_client_metadata_description",
			DefaultValue: "One or more client metadata values are invalid",
		},
	}

	// ErrorJWKSConfigurationConflict is the error returned when both jwks and jwks_uri are provided
	ErrorJWKSConfigurationConflict = tidcommon.ServiceError{
		Type: tidcommon.ClientErrorType,
		Code: "invalid_client_metadata",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.jwks_configuration_conflict",
			DefaultValue: "JWKS configuration conflict",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.jwks_configuration_conflict_description",
			DefaultValue: "Cannot specify both 'jwks' and 'jwks_uri' parameters",
		},
	}

	// ErrorServerError is the standard error for server issues
	ErrorServerError = tidcommon.ServiceError{
		Type: tidcommon.ServerErrorType,
		Code: "server_error",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.server_error",
			DefaultValue: "Server error",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.server_error_description",
			DefaultValue: "An unexpected error occurred while processing the request",
		},
	}

	// ErrorUnauthorized is the error returned when the request lacks valid authentication
	// or the authenticated caller does not hold required permissions.
	ErrorUnauthorized = tidcommon.ServiceError{
		Type: tidcommon.ClientErrorType,
		Code: "unauthorized_client",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.unauthorized",
			DefaultValue: "Unauthorized",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.unauthorized_description",
			DefaultValue: "Authentication with sufficient permissions is required to register a client",
		},
	}

	// ErrorInvalidRegistrationAccessToken is returned when the registration access token presented
	// to the client configuration endpoint is missing, malformed, expired or fails verification.
	ErrorInvalidRegistrationAccessToken = tidcommon.ServiceError{
		Type: tidcommon.ClientErrorType,
		Code: "invalid_token",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.invalid_registration_access_token",
			DefaultValue: "Invalid registration access token",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.invalid_registration_access_token_description",
			DefaultValue: "A valid registration access token is required to manage this client registration",
		},
	}

	// ErrorForbiddenRegistrationAccessToken is returned when a valid registration access token is
	// presented for a client other than the one it was issued for.
	ErrorForbiddenRegistrationAccessToken = tidcommon.ServiceError{
		Type: tidcommon.ClientErrorType,
		Code: "invalid_token",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.forbidden_registration_access_token",
			DefaultValue: "Forbidden",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.forbidden_registration_access_token_description",
			DefaultValue: "The registration access token cannot be used to manage this client registration",
		},
	}

	// ErrorClientNotFound is returned when the client referenced by the client configuration
	// endpoint does not exist.
	ErrorClientNotFound = tidcommon.ServiceError{
		Type: tidcommon.ClientErrorType,
		Code: "invalid_client_id",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.client_not_found",
			DefaultValue: "Client not found",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.client_not_found_description",
			DefaultValue: "The requested client registration could not be found",
		},
	}

	// ErrorClientIDMismatch is returned when a client configuration update request carries a
	// client_id that contradicts the one in the request path.
	ErrorClientIDMismatch = tidcommon.ServiceError{
		Type: tidcommon.ClientErrorType,
		Code: "invalid_client_metadata",
		Error: tidcommon.I18nMessage{
			Key:          "error.dcr.client_id_mismatch",
			DefaultValue: "Client ID mismatch",
		},
		ErrorDescription: tidcommon.I18nMessage{
			Key:          "error.dcr.client_id_mismatch_description",
			DefaultValue: "The client_id in the request body does not match the client being updated",
		},
	}
)
