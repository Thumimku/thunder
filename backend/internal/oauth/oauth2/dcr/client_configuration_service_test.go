// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package dcr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/internal/application/model"
	"github.com/thunder-id/thunderid/internal/system/jose/jwt"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/applicationmock"
	"github.com/thunder-id/thunderid/tests/mocks/jose/jwtmock"
	"github.com/thunder-id/thunderid/tests/mocks/oumock"
	"github.com/thunder-id/thunderid/tests/testhelpers"
)

const (
	svcTestClientID = "svc-client-id"
	svcTestAppID    = "svc-app-id"
)

// ClientConfigurationServiceTestSuite covers the RFC 7592 service operations.
type ClientConfigurationServiceTestSuite struct {
	suite.Suite
	mockAppService *applicationmock.ApplicationServiceInterfaceMock
	mockOUService  *oumock.OrganizationUnitServiceInterfaceMock
	mockJWTService *jwtmock.JWTServiceInterfaceMock
	service        DCRServiceInterface
}

func TestClientConfigurationServiceTestSuite(t *testing.T) {
	suite.Run(t, new(ClientConfigurationServiceTestSuite))
}

func (s *ClientConfigurationServiceTestSuite) SetupTest() {
	s.mockAppService = applicationmock.NewApplicationServiceInterfaceMock(s.T())
	s.mockOUService = oumock.NewOrganizationUnitServiceInterfaceMock(s.T())
	s.mockJWTService = jwtmock.NewJWTServiceInterfaceMock(s.T())
	s.service = newDCRService(s.mockAppService, s.mockOUService, nil, &MockTransactioner{},
		s.mockJWTService, testhelpers.OAuthConfig())
}

// unsignedToken builds a JWT-shaped token with the given typ header and subject. The signature is
// not meaningful: verification is mocked, and the code under test reads the header and claims.
func unsignedToken(typ, sub string) string {
	enc := func(v map[string]interface{}) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	header := enc(map[string]interface{}{"alg": "ES256", "typ": typ})
	payload := enc(map[string]interface{}{"sub": sub})
	return header + "." + payload + ".signature"
}

func (s *ClientConfigurationServiceTestSuite) existingOAuthClient() *providers.OAuthClient {
	return &providers.OAuthClient{
		ID:           svcTestAppID,
		ClientID:     svcTestClientID,
		OUID:         "ou-1",
		RedirectURIs: []string{"https://client.example.com/cb"},
		GrantTypes:   []providers.GrantType{providers.GrantTypeAuthorizationCode},
		Scopes:       []string{"openid"},
	}
}

func (s *ClientConfigurationServiceTestSuite) existingApplication() *providers.Application {
	return &providers.Application{
		ID:      svcTestAppID,
		OUID:    "ou-1",
		Name:    "Existing Client",
		Type:    "custom",
		URL:     "https://client.example.com",
		LogoURL: "https://client.example.com/logo.png",
	}
}

// UC-1: registration mints a token bound to the registered client.
func (s *ClientConfigurationServiceTestSuite) TestIssueRegistrationAccessToken_BindsToClient() {
	s.mockJWTService.On("GenerateJWT", mock.Anything, svcTestClientID, "https://thunder.io",
		mock.Anything, mock.Anything, registrationAccessTokenType, "").
		Return("issued-token", int64(0), (*tidcommon.ServiceError)(nil))

	token, err := s.service.IssueRegistrationAccessToken(context.Background(), svcTestClientID)

	s.Nil(err)
	s.Equal("issued-token", token)

	// The audience scopes the token to this client's configuration endpoint.
	call := s.mockJWTService.Calls[0]
	claims, ok := call.Arguments[4].(map[string]interface{})
	s.Require().True(ok)
	s.Equal("https://thunder.io/oauth2/dcr/register/"+svcTestClientID, claims["aud"])
}

// A valid token naming this client as its subject authorizes management of that client.
func (s *ClientConfigurationServiceTestSuite) TestValidateRegistrationAccessToken_Valid() {
	s.mockJWTService.On("VerifyJWT", mock.Anything, mock.Anything, "", "https://thunder.io").
		Return((*tidcommon.ServiceError)(nil))

	err := s.service.ValidateRegistrationAccessToken(context.Background(),
		unsignedToken(registrationAccessTokenType, svcTestClientID), svcTestClientID)

	s.Nil(err)
}

// UC-6: a token issued for another client cannot manage this one.
func (s *ClientConfigurationServiceTestSuite) TestValidateRegistrationAccessToken_OtherClient() {
	s.mockJWTService.On("VerifyJWT", mock.Anything, mock.Anything, "", "https://thunder.io").
		Return((*tidcommon.ServiceError)(nil))

	err := s.service.ValidateRegistrationAccessToken(context.Background(),
		unsignedToken(registrationAccessTokenType, "another-client"), svcTestClientID)

	s.Require().NotNil(err)
	s.Equal(ErrorForbiddenRegistrationAccessToken.Error.Key, err.Error.Key)
}

// An ordinary access token cannot be replayed as a registration access token.
func (s *ClientConfigurationServiceTestSuite) TestValidateRegistrationAccessToken_WrongType() {
	err := s.service.ValidateRegistrationAccessToken(context.Background(),
		unsignedToken("at+jwt", svcTestClientID), svcTestClientID)

	s.Require().NotNil(err)
	s.Equal(ErrorInvalidRegistrationAccessToken.Error.Key, err.Error.Key)
	s.mockJWTService.AssertNotCalled(s.T(), "VerifyJWT", mock.Anything, mock.Anything,
		mock.Anything, mock.Anything)
}

// UC-5: a token that fails verification is rejected.
func (s *ClientConfigurationServiceTestSuite) TestValidateRegistrationAccessToken_VerificationFails() {
	s.mockJWTService.On("VerifyJWT", mock.Anything, mock.Anything, "", "https://thunder.io").
		Return(&jwt.ErrorInvalidTokenSignature)

	err := s.service.ValidateRegistrationAccessToken(context.Background(),
		unsignedToken(registrationAccessTokenType, svcTestClientID), svcTestClientID)

	s.Require().NotNil(err)
	s.Equal(ErrorInvalidRegistrationAccessToken.Error.Key, err.Error.Key)
}

// A malformed token is rejected without reaching verification.
func (s *ClientConfigurationServiceTestSuite) TestValidateRegistrationAccessToken_Malformed() {
	err := s.service.ValidateRegistrationAccessToken(context.Background(), "not-a-jwt", svcTestClientID)

	s.Require().NotNil(err)
	s.Equal(ErrorInvalidRegistrationAccessToken.Error.Key, err.Error.Key)
}

// UC-2: reading a registration returns the stored metadata and the configuration fields.
func (s *ClientConfigurationServiceTestSuite) TestGetClient_ReturnsRegistration() {
	s.mockAppService.On("GetOAuthApplication", mock.Anything, svcTestClientID).
		Return(s.existingOAuthClient(), (*tidcommon.ServiceError)(nil))
	s.mockAppService.On("GetApplication", mock.Anything, svcTestAppID).
		Return(s.existingApplication(), (*tidcommon.ServiceError)(nil))
	s.mockJWTService.On("GenerateJWT", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything).
		Return("issued-token", int64(0), (*tidcommon.ServiceError)(nil))

	response, err := s.service.GetClient(context.Background(), svcTestClientID)

	s.Require().Nil(err)
	s.Equal(svcTestClientID, response.ClientID)
	s.Equal("Existing Client", response.ClientName)
	s.Equal("https://client.example.com", response.ClientURI)
	s.Equal("openid", response.Scope)
	s.Equal("issued-token", response.RegistrationAccessToken)
	s.Equal("https://thunder.io/oauth2/dcr/register/"+svcTestClientID, response.RegistrationClientURI)
	// The stored client secret cannot be read back, so it is never echoed.
	s.Empty(response.ClientSecret)
}

// A registration access token for a deleted client no longer resolves to a client.
func (s *ClientConfigurationServiceTestSuite) TestGetClient_UnknownClient() {
	s.mockAppService.On("GetOAuthApplication", mock.Anything, svcTestClientID).
		Return((*providers.OAuthClient)(nil), &tidcommon.ServiceError{
			Type: tidcommon.ClientErrorType, Code: "APP-1001",
		})

	response, err := s.service.GetClient(context.Background(), svcTestClientID)

	s.Nil(response)
	s.Require().NotNil(err)
	s.Equal(ErrorClientNotFound.Code, err.Code)
}

// UC-3: an update preserves the client identity while replacing the metadata.
func (s *ClientConfigurationServiceTestSuite) TestUpdateClient_PreservesClientIdentity() {
	s.mockAppService.On("GetOAuthApplication", mock.Anything, svcTestClientID).
		Return(s.existingOAuthClient(), (*tidcommon.ServiceError)(nil))
	s.mockAppService.On("GetApplication", mock.Anything, svcTestAppID).
		Return(s.existingApplication(), (*tidcommon.ServiceError)(nil))

	var capturedDTO *model.ApplicationDTO
	s.mockAppService.On("UpdateApplication", mock.Anything, svcTestAppID,
		mock.AnythingOfType("*model.ApplicationDTO")).
		Run(func(args mock.Arguments) {
			capturedDTO = args.Get(2).(*model.ApplicationDTO)
		}).
		Return(&model.ApplicationDTO{
			ID:   svcTestAppID,
			Name: "Renamed Client",
			InboundAuthConfig: []providers.InboundAuthConfigWithSecret{
				{
					Type: providers.OAuthInboundAuthType,
					OAuthConfig: &providers.OAuthConfigWithSecret{
						ClientID:     svcTestClientID,
						RedirectURIs: []string{"https://client.example.com/updated"},
					},
				},
			},
		}, (*tidcommon.ServiceError)(nil))
	s.mockJWTService.On("GenerateJWT", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything).
		Return("issued-token", int64(0), (*tidcommon.ServiceError)(nil))

	request := &DCRRegistrationRequest{
		ClientName:   "Renamed Client",
		RedirectURIs: []string{"https://client.example.com/updated"},
	}
	response, err := s.service.UpdateClient(context.Background(), svcTestClientID, request)

	s.Require().Nil(err)
	s.Equal(svcTestClientID, response.ClientID)
	s.Empty(response.ClientSecret, "an update must not rotate or expose the client secret")

	// The update carries the existing identity forward, so the application service preserves the
	// client_id, the client secret and the owning organization unit.
	s.Require().NotNil(capturedDTO)
	s.Equal(svcTestAppID, capturedDTO.ID)
	s.Equal("ou-1", capturedDTO.OUID)
	s.Empty(capturedDTO.Type, "the application type is immutable and must be inherited")
	s.Require().Len(capturedDTO.InboundAuthConfig, 1)
	s.Equal(svcTestClientID, capturedDTO.InboundAuthConfig[0].OAuthConfig.ClientID)
	s.Empty(capturedDTO.InboundAuthConfig[0].OAuthConfig.ClientSecret)
}

// An update that omits the client name keeps the registered one, since a name is required.
func (s *ClientConfigurationServiceTestSuite) TestUpdateClient_InheritsNameWhenOmitted() {
	s.mockAppService.On("GetOAuthApplication", mock.Anything, svcTestClientID).
		Return(s.existingOAuthClient(), (*tidcommon.ServiceError)(nil))
	s.mockAppService.On("GetApplication", mock.Anything, svcTestAppID).
		Return(s.existingApplication(), (*tidcommon.ServiceError)(nil))

	var capturedDTO *model.ApplicationDTO
	s.mockAppService.On("UpdateApplication", mock.Anything, svcTestAppID,
		mock.AnythingOfType("*model.ApplicationDTO")).
		Run(func(args mock.Arguments) {
			capturedDTO = args.Get(2).(*model.ApplicationDTO)
		}).
		Return(&model.ApplicationDTO{
			ID:   svcTestAppID,
			Name: "Existing Client",
			InboundAuthConfig: []providers.InboundAuthConfigWithSecret{
				{
					Type:        providers.OAuthInboundAuthType,
					OAuthConfig: &providers.OAuthConfigWithSecret{ClientID: svcTestClientID},
				},
			},
		}, (*tidcommon.ServiceError)(nil))
	s.mockJWTService.On("GenerateJWT", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything).
		Return("issued-token", int64(0), (*tidcommon.ServiceError)(nil))

	request := &DCRRegistrationRequest{RedirectURIs: []string{"https://client.example.com/cb"}}
	_, err := s.service.UpdateClient(context.Background(), svcTestClientID, request)

	s.Require().Nil(err)
	s.Require().NotNil(capturedDTO)
	s.Equal("Existing Client", capturedDTO.Name)
}

// Updating an unknown client reports not found rather than creating one.
func (s *ClientConfigurationServiceTestSuite) TestUpdateClient_UnknownClient() {
	s.mockAppService.On("GetOAuthApplication", mock.Anything, svcTestClientID).
		Return((*providers.OAuthClient)(nil), &tidcommon.ServiceError{
			Type: tidcommon.ClientErrorType, Code: "APP-1001",
		})

	response, err := s.service.UpdateClient(context.Background(), svcTestClientID,
		&DCRRegistrationRequest{RedirectURIs: []string{"https://client.example.com/cb"}})

	s.Nil(response)
	s.Require().NotNil(err)
	s.Equal(ErrorClientNotFound.Code, err.Code)
	s.mockAppService.AssertNotCalled(s.T(), "UpdateApplication", mock.Anything, mock.Anything,
		mock.Anything)
}

// Conflicting JWKS configuration is rejected on update, as it is on registration.
func (s *ClientConfigurationServiceTestSuite) TestUpdateClient_JWKSConflict() {
	response, err := s.service.UpdateClient(context.Background(), svcTestClientID,
		&DCRRegistrationRequest{
			RedirectURIs: []string{"https://client.example.com/cb"},
			JWKSUri:      "https://client.example.com/jwks.json",
			JWKS:         map[string]interface{}{"keys": []interface{}{}},
		})

	s.Nil(response)
	s.Require().NotNil(err)
	s.Equal(ErrorJWKSConfigurationConflict.Code, err.Code)
}

// UC-4: deleting a registration removes the underlying application.
func (s *ClientConfigurationServiceTestSuite) TestDeleteClient_RemovesApplication() {
	s.mockAppService.On("GetOAuthApplication", mock.Anything, svcTestClientID).
		Return(s.existingOAuthClient(), (*tidcommon.ServiceError)(nil))
	s.mockAppService.On("GetApplication", mock.Anything, svcTestAppID).
		Return(s.existingApplication(), (*tidcommon.ServiceError)(nil))
	s.mockAppService.On("DeleteApplication", mock.Anything, svcTestAppID).
		Return((*tidcommon.ServiceError)(nil))

	err := s.service.DeleteClient(context.Background(), svcTestClientID)

	s.Nil(err)
	s.mockAppService.AssertCalled(s.T(), "DeleteApplication", mock.Anything, svcTestAppID)
}

// Deleting an unknown client reports not found.
func (s *ClientConfigurationServiceTestSuite) TestDeleteClient_UnknownClient() {
	s.mockAppService.On("GetOAuthApplication", mock.Anything, svcTestClientID).
		Return((*providers.OAuthClient)(nil), &tidcommon.ServiceError{
			Type: tidcommon.ClientErrorType, Code: "APP-1001",
		})

	err := s.service.DeleteClient(context.Background(), svcTestClientID)

	s.Require().NotNil(err)
	s.Equal(ErrorClientNotFound.Code, err.Code)
	s.mockAppService.AssertNotCalled(s.T(), "DeleteApplication", mock.Anything, mock.Anything)
}
