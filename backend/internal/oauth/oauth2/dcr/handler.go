// Copyright 2025 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package dcr

import (
	"context"
	"net/http"

	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"

	oauthconfig "github.com/thunder-id/thunderid/internal/oauth/config"
	"github.com/thunder-id/thunderid/internal/system/log"
	"github.com/thunder-id/thunderid/internal/system/security"
	sysutils "github.com/thunder-id/thunderid/internal/system/utils"
)

// dcrHandler defines the handler for DCR API requests.
type dcrHandler struct {
	dcrInsecure bool
	dcrService  DCRServiceInterface
}

// newDCRHandler creates a new instance of dcrHandler.
func newDCRHandler(dcrService DCRServiceInterface, cfg oauthconfig.Config) *dcrHandler {
	return &dcrHandler{
		dcrInsecure: cfg.OAuth.DCR.Insecure,
		dcrService:  dcrService,
	}
}

// HandleDCRRegistration handles the DCR client registration request.
func (dh *dcrHandler) HandleDCRRegistration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// When DCR is not insecure, require a valid token with required permissions.
	if !dh.dcrInsecure && !dh.checkDCRAuthorization(r, w) {
		return
	}

	dcrRequest, err := sysutils.DecodeJSONBody[DCRRegistrationRequest](r)
	if err != nil {
		sysutils.WriteJSONError(ctx, w, ErrorInvalidRequestFormat.Code,
			ErrorInvalidRequestFormat.ErrorDescription.DefaultValue, http.StatusBadRequest, nil)
		return
	}

	dcrResponse, svcErr := dh.dcrService.RegisterClient(ctx, dcrRequest)
	if svcErr != nil {
		if svcErr.Type == tidcommon.ServerErrorType {
			logger := log.GetLogger().With(log.String(log.LoggerKeyComponentName, "DCRHandler"))
			logger.Error(ctx, "Internal server error processing DCR registration request",
				log.MaskedString("client_name", dcrRequest.ClientName),
				log.String("error_code", svcErr.Code),
				log.String("error", svcErr.Error.DefaultValue),
			)
		}
		dh.writeServiceErrorResponse(ctx, w, svcErr)
		return
	}

	sysutils.WriteSuccessResponse(ctx, w, http.StatusCreated, dcrResponse)
}

// checkDCRAuthorization verifies that the caller holds required permission.
// Returns true if authorized, false (and writes an HTTP 401) otherwise.
func (dh *dcrHandler) checkDCRAuthorization(r *http.Request, w http.ResponseWriter) bool {
	if security.HasSystemPermission(security.GetPermissions(r.Context())) {
		return true
	}
	sysutils.WriteJSONError(r.Context(), w, ErrorUnauthorized.Code,
		ErrorUnauthorized.ErrorDescription.DefaultValue, http.StatusUnauthorized, nil)
	return false
}

// HandleGetClientConfiguration handles an RFC 7592 read of a client's registration.
func (dh *dcrHandler) HandleGetClientConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clientID, ok := dh.authorizeClientConfigurationRequest(r, w)
	if !ok {
		return
	}

	response, svcErr := dh.dcrService.GetClient(ctx, clientID)
	if svcErr != nil {
		dh.writeClientConfigurationError(ctx, w, svcErr, "read")
		return
	}

	sysutils.WriteSuccessResponse(ctx, w, http.StatusOK, response)
}

// HandleUpdateClientConfiguration handles an RFC 7592 update of a client's registration.
func (dh *dcrHandler) HandleUpdateClientConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clientID, ok := dh.authorizeClientConfigurationRequest(r, w)
	if !ok {
		return
	}

	dcrRequest, err := sysutils.DecodeJSONBody[DCRRegistrationRequest](r)
	if err != nil {
		sysutils.WriteJSONError(ctx, w, ErrorInvalidRequestFormat.Code,
			ErrorInvalidRequestFormat.ErrorDescription.DefaultValue, http.StatusBadRequest, nil)
		return
	}
	// RFC 7592 requires the client to include its client_id, and it must identify the client
	// being updated. Server-managed registration fields are ignored rather than applied.
	if dcrRequest.ClientID != "" && dcrRequest.ClientID != clientID {
		sysutils.WriteJSONError(ctx, w, ErrorClientIDMismatch.Code,
			ErrorClientIDMismatch.ErrorDescription.DefaultValue, http.StatusBadRequest, nil)
		return
	}

	response, svcErr := dh.dcrService.UpdateClient(ctx, clientID, dcrRequest)
	if svcErr != nil {
		dh.writeClientConfigurationError(ctx, w, svcErr, "update")
		return
	}

	sysutils.WriteSuccessResponse(ctx, w, http.StatusOK, response)
}

// HandleDeleteClientConfiguration handles an RFC 7592 deletion of a client's registration.
func (dh *dcrHandler) HandleDeleteClientConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clientID, ok := dh.authorizeClientConfigurationRequest(r, w)
	if !ok {
		return
	}

	if svcErr := dh.dcrService.DeleteClient(ctx, clientID); svcErr != nil {
		dh.writeClientConfigurationError(ctx, w, svcErr, "delete")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// authorizeClientConfigurationRequest resolves the client ID from the request path and authorizes
// the caller, either by a registration access token bound to that client or by a system permission.
// It returns the client ID and true when the request may proceed; otherwise it writes the error
// response and returns false.
func (dh *dcrHandler) authorizeClientConfigurationRequest(
	r *http.Request, w http.ResponseWriter) (string, bool) {
	ctx := r.Context()

	clientID := r.PathValue("client_id")
	if clientID == "" {
		sysutils.WriteJSONError(ctx, w, ErrorClientNotFound.Code,
			ErrorClientNotFound.ErrorDescription.DefaultValue, http.StatusNotFound, nil)
		return "", false
	}

	token, err := sysutils.ExtractBearerToken(r.Header.Get(authorizationHeaderName))
	if err == nil && token != "" {
		svcErr := dh.dcrService.ValidateRegistrationAccessToken(ctx, token, clientID)
		if svcErr == nil {
			return clientID, true
		}
		// A token that is valid but issued for another client is a permission failure, and must
		// not fall through to the administrative path. Both registration token errors share the
		// RFC 6750 "invalid_token" code, so they are distinguished by their message key.
		if svcErr.Error.Key == ErrorForbiddenRegistrationAccessToken.Error.Key {
			sysutils.WriteJSONError(ctx, w, svcErr.Code,
				svcErr.ErrorDescription.DefaultValue, http.StatusForbidden, nil)
			return "", false
		}
	}

	// Fall back to an administrative caller holding the system permission.
	if security.HasSystemPermission(security.GetPermissions(ctx)) {
		return clientID, true
	}

	w.Header().Set(wwwAuthenticateHeaderName, wwwAuthenticateInvalidToken)
	sysutils.WriteJSONError(ctx, w, ErrorInvalidRegistrationAccessToken.Code,
		ErrorInvalidRegistrationAccessToken.ErrorDescription.DefaultValue, http.StatusUnauthorized, nil)
	return "", false
}

// writeClientConfigurationError writes an RFC 7592 error response for a client configuration
// operation, logging server errors.
func (dh *dcrHandler) writeClientConfigurationError(ctx context.Context, w http.ResponseWriter,
	svcErr *tidcommon.ServiceError, operation string) {
	if svcErr.Type == tidcommon.ServerErrorType {
		logger := log.GetLogger().With(log.String(log.LoggerKeyComponentName, "DCRHandler"))
		logger.Error(ctx, "Internal server error processing client configuration request",
			log.String("operation", operation),
			log.String("error_code", svcErr.Code),
			log.String("error", svcErr.Error.DefaultValue),
		)
	}
	if svcErr.Code == ErrorClientNotFound.Code {
		sysutils.WriteJSONError(ctx, w, svcErr.Code,
			svcErr.ErrorDescription.DefaultValue, http.StatusNotFound, nil)
		return
	}
	dh.writeServiceErrorResponse(ctx, w, svcErr)
}

// writeServiceErrorResponse writes a service error response.
func (
	dh *dcrHandler) writeServiceErrorResponse(ctx context.Context,
	w http.ResponseWriter,
	svcErr *tidcommon.ServiceError) {
	var statusCode int

	switch svcErr.Type {
	case tidcommon.ClientErrorType:
		statusCode = http.StatusBadRequest
	case tidcommon.ServerErrorType:
		statusCode = http.StatusInternalServerError
	default:
		statusCode = http.StatusBadRequest
	}

	sysutils.WriteJSONError(ctx, w, svcErr.Code, svcErr.ErrorDescription.DefaultValue, statusCode, nil)
}
