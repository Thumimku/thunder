// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package dcr

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/tests/integration/testutils"
)

// DCRMTestSuite covers the RFC 7592 client configuration endpoint: reading, updating and deleting
// a dynamically registered client using the registration access token issued at registration.
type DCRMTestSuite struct {
	suite.Suite
	registeredAppIDs []string
}

func TestDCRMTestSuite(t *testing.T) {
	suite.Run(t, new(DCRMTestSuite))
}

func (ts *DCRMTestSuite) TearDownSuite() {
	for _, appID := range ts.registeredAppIDs {
		if appID != "" {
			if err := testutils.DeleteApplication(appID); err != nil {
				ts.T().Logf("Failed to delete application during teardown: %v", err)
			}
		}
	}
}

// UC-1: a successful registration returns the registration access token and client configuration
// URI needed to manage the registration.
func (ts *DCRMTestSuite) TestRegistrationReturnsClientConfigurationFields() {
	response := ts.register("DCRM Registration Fields")
	ts.registeredAppIDs = append(ts.registeredAppIDs, response.AppID)

	ts.Assert().NotEmpty(response.RegistrationAccessToken,
		"registration must return a registration access token")
	ts.Assert().Equal(testServerURL+dcrEndpoint+"/"+response.ClientID, response.RegistrationClientURI)
	ts.Assert().NotZero(response.ClientIDIssuedAt)
	ts.Assert().NotEqual(response.ClientSecret, response.RegistrationAccessToken,
		"the registration access token must not be the client secret")
}

// UC-2: a client reads its own registration with its registration access token.
func (ts *DCRMTestSuite) TestReadClientRegistration() {
	registered := ts.register("DCRM Read Client")
	ts.registeredAppIDs = append(ts.registeredAppIDs, registered.AppID)

	read, status, _ := ts.manage(http.MethodGet, registered.ClientID,
		registered.RegistrationAccessToken, nil)

	ts.Require().Equal(http.StatusOK, status)
	ts.Assert().Equal(registered.ClientID, read.ClientID)
	ts.Assert().Equal("DCRM Read Client", read.ClientName)
	ts.Assert().Contains(read.RedirectURIs, "https://dcrm.example.com/callback")
	ts.Assert().Empty(read.ClientSecret, "the client secret is not readable and must not be returned")
}

// UC-3: a client updates its registered metadata, and the client identity survives the update.
func (ts *DCRMTestSuite) TestUpdateClientRegistration() {
	registered := ts.register("DCRM Update Client")
	ts.registeredAppIDs = append(ts.registeredAppIDs, registered.AppID)

	update := DCRRegistrationRequest{
		ClientID:                registered.ClientID,
		ClientName:              "DCRM Updated Client",
		RedirectURIs:            []string{"https://dcrm.example.com/updated"},
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_basic",
	}
	body, err := json.Marshal(update)
	ts.Require().NoError(err)

	updated, status, _ := ts.manage(http.MethodPut, registered.ClientID,
		registered.RegistrationAccessToken, body)

	ts.Require().Equal(http.StatusOK, status)
	ts.Assert().Equal(registered.ClientID, updated.ClientID, "the client_id must be preserved")
	ts.Assert().Equal("DCRM Updated Client", updated.ClientName)

	// The change is persisted, and the identity still holds on a subsequent read.
	read, status, _ := ts.manage(http.MethodGet, registered.ClientID,
		registered.RegistrationAccessToken, nil)
	ts.Require().Equal(http.StatusOK, status)
	ts.Assert().Equal(registered.ClientID, read.ClientID)
	ts.Assert().Equal("DCRM Updated Client", read.ClientName)
	ts.Assert().Contains(read.RedirectURIs, "https://dcrm.example.com/updated")
	ts.Assert().NotContains(read.RedirectURIs, "https://dcrm.example.com/callback")
}

// The client secret is preserved across an update. Since it cannot be read back, this is verified
// by exercising it: the original secret still authenticates at the token endpoint afterwards.
func (ts *DCRMTestSuite) TestUpdatePreservesClientSecret() {
	registered := ts.register("DCRM Secret Preserved")
	ts.registeredAppIDs = append(ts.registeredAppIDs, registered.AppID)
	ts.Require().NotEmpty(registered.ClientSecret)

	update := DCRRegistrationRequest{
		ClientID:                registered.ClientID,
		ClientName:              "DCRM Secret Preserved Renamed",
		RedirectURIs:            []string{"https://dcrm.example.com/callback"},
		GrantTypes:              []string{"client_credentials"},
		TokenEndpointAuthMethod: "client_secret_basic",
	}
	body, err := json.Marshal(update)
	ts.Require().NoError(err)

	_, status, _ := ts.manage(http.MethodPut, registered.ClientID,
		registered.RegistrationAccessToken, body)
	ts.Require().Equal(http.StatusOK, status)

	ts.Assert().True(ts.clientCredentialsSucceeds(registered.ClientID, registered.ClientSecret),
		"the original client secret must still authenticate after an update")
}

// UC-4: a client deletes its registration, after which the registration is gone and its
// registration access token no longer resolves to a client.
func (ts *DCRMTestSuite) TestDeleteClientRegistration() {
	registered := ts.register("DCRM Delete Client")

	_, status, _ := ts.manage(http.MethodDelete, registered.ClientID,
		registered.RegistrationAccessToken, nil)
	ts.Require().Equal(http.StatusNoContent, status)

	_, status, _ = ts.manage(http.MethodGet, registered.ClientID,
		registered.RegistrationAccessToken, nil)
	ts.Assert().Equal(http.StatusNotFound, status,
		"the registration access token must be unusable once the client is deleted")
}

// UC-5: an invalid registration access token is rejected and does not expose the registration.
func (ts *DCRMTestSuite) TestInvalidRegistrationAccessTokenIsRejected() {
	registered := ts.register("DCRM Invalid Token")
	ts.registeredAppIDs = append(ts.registeredAppIDs, registered.AppID)

	_, status, _ := ts.manage(http.MethodGet, registered.ClientID, "not-a-valid-token", nil)
	ts.Assert().Equal(http.StatusUnauthorized, status)
}

// A request carrying no credentials at all is rejected.
func (ts *DCRMTestSuite) TestMissingRegistrationAccessTokenIsRejected() {
	registered := ts.register("DCRM Missing Token")
	ts.registeredAppIDs = append(ts.registeredAppIDs, registered.AppID)

	_, status, _ := ts.manage(http.MethodGet, registered.ClientID, "", nil)
	ts.Assert().Equal(http.StatusUnauthorized, status)
}

// UC-6: a registration access token issued for one client cannot manage another client.
func (ts *DCRMTestSuite) TestTokenCannotManageAnotherClient() {
	first := ts.register("DCRM Token Owner")
	ts.registeredAppIDs = append(ts.registeredAppIDs, first.AppID)
	second := ts.register("DCRM Other Client")
	ts.registeredAppIDs = append(ts.registeredAppIDs, second.AppID)

	// Present the second client's token against the first client's registration.
	_, status, _ := ts.manage(http.MethodGet, first.ClientID, second.RegistrationAccessToken, nil)
	ts.Assert().Equal(http.StatusForbidden, status)

	// The first client's registration is untouched and still readable by its own token.
	read, status, _ := ts.manage(http.MethodGet, first.ClientID, first.RegistrationAccessToken, nil)
	ts.Require().Equal(http.StatusOK, status)
	ts.Assert().Equal("DCRM Token Owner", read.ClientName)
}

// An update naming a different client is rejected.
func (ts *DCRMTestSuite) TestUpdateWithMismatchedClientIDIsRejected() {
	registered := ts.register("DCRM Mismatch Client")
	ts.registeredAppIDs = append(ts.registeredAppIDs, registered.AppID)

	body, err := json.Marshal(DCRRegistrationRequest{
		ClientID:     "some-other-client-id",
		ClientName:   "DCRM Mismatch Client",
		RedirectURIs: []string{"https://dcrm.example.com/callback"},
	})
	ts.Require().NoError(err)

	_, status, _ := ts.manage(http.MethodPut, registered.ClientID,
		registered.RegistrationAccessToken, body)
	ts.Assert().Equal(http.StatusBadRequest, status)
}

// An update carrying invalid client metadata is rejected.
func (ts *DCRMTestSuite) TestUpdateWithInvalidMetadataIsRejected() {
	registered := ts.register("DCRM Invalid Metadata")
	ts.registeredAppIDs = append(ts.registeredAppIDs, registered.AppID)

	body, err := json.Marshal(DCRRegistrationRequest{
		ClientID:     registered.ClientID,
		ClientName:   "DCRM Invalid Metadata",
		RedirectURIs: []string{"not-a-valid-uri"},
	})
	ts.Require().NoError(err)

	_, status, errResponse := ts.manage(http.MethodPut, registered.ClientID,
		registered.RegistrationAccessToken, body)
	ts.Require().Equal(http.StatusBadRequest, status)
	ts.Require().NotNil(errResponse)
	ts.Assert().NotEmpty(errResponse.Error)
}

// A token presented for a client it was not issued for is rejected before the client is looked up,
// so an unknown client identifier is reported as forbidden rather than not found.
func (ts *DCRMTestSuite) TestUnknownClientIsRejected() {
	registered := ts.register("DCRM Unknown Client Probe")
	ts.registeredAppIDs = append(ts.registeredAppIDs, registered.AppID)

	_, status, _ := ts.manage(http.MethodGet, "client-that-does-not-exist",
		registered.RegistrationAccessToken, nil)
	ts.Assert().Equal(http.StatusForbidden, status)
}

// register creates a client through DCR and fails the test if registration does not succeed.
func (ts *DCRMTestSuite) register(clientName string) *DCRRegistrationResponse {
	request := DCRRegistrationRequest{
		ClientName:              clientName,
		RedirectURIs:            []string{"https://dcrm.example.com/callback"},
		GrantTypes:              []string{"authorization_code", "client_credentials"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_basic",
	}
	requestJSON, err := json.Marshal(request)
	ts.Require().NoError(err)

	req, err := http.NewRequest(http.MethodPost, testServerURL+dcrEndpoint, bytes.NewReader(requestJSON))
	ts.Require().NoError(err)
	req.Header.Set("Content-Type", "application/json")
	ts.setAdminAuthorization(req)

	resp, err := testutils.GetHTTPClient().Do(req)
	ts.Require().NoError(err)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		responseBody, _ := io.ReadAll(resp.Body)
		ts.T().Fatalf("Expected status 201 registering %q, got %d. Response: %s",
			clientName, resp.StatusCode, string(responseBody))
	}

	var response DCRRegistrationResponse
	ts.Require().NoError(json.NewDecoder(resp.Body).Decode(&response))
	ts.Require().NotEmpty(response.ClientID)
	return &response
}

// manage issues a request to the client configuration endpoint authorized by the given
// registration access token, and returns the decoded response, the status and any error body.
func (ts *DCRMTestSuite) manage(method, clientID, registrationAccessToken string, body []byte) (
	*DCRRegistrationResponse, int, *DCRErrorResponse) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, testServerURL+dcrEndpoint+"/"+clientID, reader)
	ts.Require().NoError(err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if registrationAccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+registrationAccessToken)
	}

	// A plain client is used so that no administrative token is attached: these requests must be
	// authorized by the registration access token alone.
	resp, err := testutils.GetRawHTTPClient().Do(req)
	ts.Require().NoError(err)
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	ts.Require().NoError(err)

	if resp.StatusCode == http.StatusNoContent || len(responseBody) == 0 {
		return nil, resp.StatusCode, nil
	}
	if resp.StatusCode >= http.StatusBadRequest {
		var errResponse DCRErrorResponse
		if err := json.Unmarshal(responseBody, &errResponse); err != nil {
			return nil, resp.StatusCode, nil
		}
		return nil, resp.StatusCode, &errResponse
	}

	var response DCRRegistrationResponse
	ts.Require().NoError(json.Unmarshal(responseBody, &response))
	return &response, resp.StatusCode, nil
}

// clientCredentialsSucceeds reports whether the given credentials obtain a token, which is how the
// client secret is verified without reading it back.
func (ts *DCRMTestSuite) clientCredentialsSucceeds(clientID, clientSecret string) bool {
	form := "grant_type=client_credentials"
	req, err := http.NewRequest(http.MethodPost, testServerURL+"/oauth2/token",
		bytes.NewReader([]byte(form)))
	ts.Require().NoError(err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

	resp, err := testutils.GetRawHTTPClient().Do(req)
	ts.Require().NoError(err)
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	return resp.StatusCode == http.StatusOK
}

// setAdminAuthorization attaches an administrative access token, which DCR registration requires
// because it does not run in insecure mode.
func (ts *DCRMTestSuite) setAdminAuthorization(req *http.Request) {
	token, err := testutils.GetAccessToken()
	if err != nil {
		ts.T().Fatalf("Failed to obtain access token: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
}
