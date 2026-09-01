// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package dcr

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/internal/system/config"
	engineconfig "github.com/thunder-id/thunderid/pkg/thunderidengine/config"

	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/tests/testhelpers"
)

const (
	testClientID   = "test-client-id"
	testRATHeader  = "Bearer registration-access-token"
	testConfigPath = "/oauth2/dcr/register/" + testClientID
)

// ClientConfigurationHandlerTestSuite covers the RFC 7592 client configuration endpoint handlers.
type ClientConfigurationHandlerTestSuite struct {
	suite.Suite
	mockService *DCRServiceInterfaceMock
	handler     *dcrHandler
}

func TestClientConfigurationHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(ClientConfigurationHandlerTestSuite))
}

func (s *ClientConfigurationHandlerTestSuite) SetupTest() {
	s.mockService = NewDCRServiceInterfaceMock(s.T())
	_ = config.InitializeServerRuntime("test", &config.Config{
		OAuth: config.OAuthConfig{DCR: engineconfig.DCRConfig{Insecure: true}},
	})
	s.handler = newDCRHandler(s.mockService, testhelpers.OAuthConfig())
}

func (s *ClientConfigurationHandlerTestSuite) TearDownTest() {
	config.ResetServerRuntime()
}

// newRequest builds a client configuration request carrying the given Authorization header and
// with the client_id path value populated, as the router would.
func (s *ClientConfigurationHandlerTestSuite) newRequest(
	method, authHeader string, body []byte) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, testConfigPath, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, testConfigPath, nil)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	req.SetPathValue("client_id", testClientID)
	return req
}

// expectValidRAT primes the service to accept the registration access token for testClientID.
func (s *ClientConfigurationHandlerTestSuite) expectValidRAT() {
	s.mockService.On("ValidateRegistrationAccessToken", mock.Anything, mock.Anything, testClientID).
		Return((*tidcommon.ServiceError)(nil))
}

// UC-2: a client reads its registration with a valid registration access token.
func (s *ClientConfigurationHandlerTestSuite) TestGet_ReturnsRegistration() {
	s.expectValidRAT()
	s.mockService.On("GetClient", mock.Anything, testClientID).
		Return(&DCRRegistrationResponse{ClientID: testClientID, ClientName: "Test Client"},
			(*tidcommon.ServiceError)(nil))

	rr := httptest.NewRecorder()
	s.handler.HandleGetClientConfiguration(rr, s.newRequest(http.MethodGet, testRATHeader, nil))

	s.Equal(http.StatusOK, rr.Code)
	var response DCRRegistrationResponse
	s.Require().NoError(json.Unmarshal(rr.Body.Bytes(), &response))
	s.Equal(testClientID, response.ClientID)
	s.Empty(response.ClientSecret, "the client secret is not readable and must not be returned")
}

// UC-5: an invalid registration access token is rejected without exposing the registration.
func (s *ClientConfigurationHandlerTestSuite) TestGet_InvalidTokenIsUnauthorized() {
	s.mockService.On("ValidateRegistrationAccessToken", mock.Anything, mock.Anything, testClientID).
		Return(&ErrorInvalidRegistrationAccessToken)

	rr := httptest.NewRecorder()
	s.handler.HandleGetClientConfiguration(rr, s.newRequest(http.MethodGet, "Bearer bad-token", nil))

	s.Equal(http.StatusUnauthorized, rr.Code)
	s.Equal(wwwAuthenticateInvalidToken, rr.Header().Get(wwwAuthenticateHeaderName))
	s.mockService.AssertNotCalled(s.T(), "GetClient", mock.Anything, mock.Anything)
}

// A request without any credentials is rejected.
func (s *ClientConfigurationHandlerTestSuite) TestGet_MissingTokenIsUnauthorized() {
	rr := httptest.NewRecorder()
	s.handler.HandleGetClientConfiguration(rr, s.newRequest(http.MethodGet, "", nil))

	s.Equal(http.StatusUnauthorized, rr.Code)
	s.mockService.AssertNotCalled(s.T(), "GetClient", mock.Anything, mock.Anything)
}

// UC-6: a token issued for another client cannot manage this registration.
func (s *ClientConfigurationHandlerTestSuite) TestGet_TokenForAnotherClientIsForbidden() {
	s.mockService.On("ValidateRegistrationAccessToken", mock.Anything, mock.Anything, testClientID).
		Return(&ErrorForbiddenRegistrationAccessToken)

	rr := httptest.NewRecorder()
	s.handler.HandleGetClientConfiguration(rr, s.newRequest(http.MethodGet, testRATHeader, nil))

	s.Equal(http.StatusForbidden, rr.Code)
	s.mockService.AssertNotCalled(s.T(), "GetClient", mock.Anything, mock.Anything)
}

// A registration access token for a deleted client resolves to no client, so it is inert.
func (s *ClientConfigurationHandlerTestSuite) TestGet_UnknownClientIsNotFound() {
	s.expectValidRAT()
	s.mockService.On("GetClient", mock.Anything, testClientID).
		Return((*DCRRegistrationResponse)(nil), &ErrorClientNotFound)

	rr := httptest.NewRecorder()
	s.handler.HandleGetClientConfiguration(rr, s.newRequest(http.MethodGet, testRATHeader, nil))

	s.Equal(http.StatusNotFound, rr.Code)
}

// UC-3: a client updates its registered metadata.
func (s *ClientConfigurationHandlerTestSuite) TestPut_UpdatesRegistration() {
	s.expectValidRAT()
	s.mockService.On("UpdateClient", mock.Anything, testClientID,
		mock.AnythingOfType("*dcr.DCRRegistrationRequest")).
		Return(&DCRRegistrationResponse{ClientID: testClientID, ClientName: "Renamed"},
			(*tidcommon.ServiceError)(nil))

	body := []byte(`{"client_id":"` + testClientID + `","client_name":"Renamed",
		"redirect_uris":["https://client.example.com/cb"]}`)
	rr := httptest.NewRecorder()
	s.handler.HandleUpdateClientConfiguration(rr, s.newRequest(http.MethodPut, testRATHeader, body))

	s.Equal(http.StatusOK, rr.Code)
	var response DCRRegistrationResponse
	s.Require().NoError(json.Unmarshal(rr.Body.Bytes(), &response))
	s.Equal(testClientID, response.ClientID, "the client_id must be preserved across an update")
	s.Equal("Renamed", response.ClientName)
}

// A client_id in the body that contradicts the path is rejected.
func (s *ClientConfigurationHandlerTestSuite) TestPut_ClientIDMismatchIsRejected() {
	s.expectValidRAT()

	body := []byte(`{"client_id":"some-other-client","redirect_uris":["https://client.example.com/cb"]}`)
	rr := httptest.NewRecorder()
	s.handler.HandleUpdateClientConfiguration(rr, s.newRequest(http.MethodPut, testRATHeader, body))

	s.Equal(http.StatusBadRequest, rr.Code)
	s.mockService.AssertNotCalled(s.T(), "UpdateClient", mock.Anything, mock.Anything, mock.Anything)
}

// A malformed body is rejected before reaching the service.
func (s *ClientConfigurationHandlerTestSuite) TestPut_InvalidBodyIsRejected() {
	s.expectValidRAT()

	rr := httptest.NewRecorder()
	s.handler.HandleUpdateClientConfiguration(rr,
		s.newRequest(http.MethodPut, testRATHeader, []byte(`{"invalid": json}`)))

	s.Equal(http.StatusBadRequest, rr.Code)
	s.mockService.AssertNotCalled(s.T(), "UpdateClient", mock.Anything, mock.Anything, mock.Anything)
}

// Invalid client metadata on update is surfaced as an RFC 7592 client error.
func (s *ClientConfigurationHandlerTestSuite) TestPut_InvalidMetadataIsRejected() {
	s.expectValidRAT()
	s.mockService.On("UpdateClient", mock.Anything, testClientID,
		mock.AnythingOfType("*dcr.DCRRegistrationRequest")).
		Return((*DCRRegistrationResponse)(nil), &ErrorInvalidRedirectURI)

	body := []byte(`{"redirect_uris":["not-a-uri"]}`)
	rr := httptest.NewRecorder()
	s.handler.HandleUpdateClientConfiguration(rr, s.newRequest(http.MethodPut, testRATHeader, body))

	s.Equal(http.StatusBadRequest, rr.Code)
	var errResponse DCRErrorResponse
	s.Require().NoError(json.Unmarshal(rr.Body.Bytes(), &errResponse))
	s.Equal(ErrorInvalidRedirectURI.Code, errResponse.Error)
}

// UC-4: a client deletes its registration.
func (s *ClientConfigurationHandlerTestSuite) TestDelete_RemovesRegistration() {
	s.expectValidRAT()
	s.mockService.On("DeleteClient", mock.Anything, testClientID).
		Return((*tidcommon.ServiceError)(nil))

	rr := httptest.NewRecorder()
	s.handler.HandleDeleteClientConfiguration(rr, s.newRequest(http.MethodDelete, testRATHeader, nil))

	s.Equal(http.StatusNoContent, rr.Code)
	s.Empty(rr.Body.Bytes())
}

// Deleting an unknown client reports not found.
func (s *ClientConfigurationHandlerTestSuite) TestDelete_UnknownClientIsNotFound() {
	s.expectValidRAT()
	s.mockService.On("DeleteClient", mock.Anything, testClientID).Return(&ErrorClientNotFound)

	rr := httptest.NewRecorder()
	s.handler.HandleDeleteClientConfiguration(rr, s.newRequest(http.MethodDelete, testRATHeader, nil))

	s.Equal(http.StatusNotFound, rr.Code)
}

// A server failure during deletion is reported as a server error.
func (s *ClientConfigurationHandlerTestSuite) TestDelete_ServerErrorIsReported() {
	s.expectValidRAT()
	s.mockService.On("DeleteClient", mock.Anything, testClientID).Return(&ErrorServerError)

	rr := httptest.NewRecorder()
	s.handler.HandleDeleteClientConfiguration(rr, s.newRequest(http.MethodDelete, testRATHeader, nil))

	s.Equal(http.StatusInternalServerError, rr.Code)
}

// A request without a client ID in the path cannot identify a registration.
func (s *ClientConfigurationHandlerTestSuite) TestGet_MissingClientIDIsNotFound() {
	req := httptest.NewRequest(http.MethodGet, "/oauth2/dcr/register/", nil)
	req.Header.Set("Authorization", testRATHeader)

	rr := httptest.NewRecorder()
	s.handler.HandleGetClientConfiguration(rr, req)

	s.Equal(http.StatusNotFound, rr.Code)
}
