// Copyright © 2019 Banzai Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package auth

import (
	"context"
	"fmt"

	pkgAuth "github.com/banzaicloud/pipeline/pkg/auth"
	pkgSecret "github.com/banzaicloud/pipeline/pkg/secret"
	"github.com/banzaicloud/pipeline/secret"
	"github.com/dexidp/dex/api"
	"github.com/goph/emperror"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type dexClient struct {
	api.DexClient
	grpcConn *grpc.ClientConn
}

func (d *dexClient) Close() error {
	return d.grpcConn.Close()
}

func newDexClient(hostAndPort, caPath string) (*dexClient, error) {
	dialOption := grpc.WithInsecure()
	if caPath != "" {
		creds, err := credentials.NewClientTLSFromFile(caPath, "")
		if err != nil {
			return nil, fmt.Errorf("load dex cert: %v", err)
		}
		dialOption = grpc.WithTransportCredentials(creds)
	}
	conn, err := grpc.Dial(hostAndPort, dialOption)
	if err != nil {
		return nil, fmt.Errorf("dail: %v", err)
	}
	return &dexClient{DexClient: api.NewDexClient(conn), grpcConn: conn}, nil
}

type ClusterAuthService interface {
	RegisterCluster(pkgAuth.OrganizationID, string) error
	UnRegisterCluster(string) error
}

type noOpClusterAuthService struct {
}

func NewNoOpClusterAuthService() (ClusterAuthService, error) {
	return &noOpClusterAuthService{}, nil
}

func (*noOpClusterAuthService) UnRegisterCluster(clusterUID string) error {
	return nil
}

func (*noOpClusterAuthService) RegisterCluster(orgID pkgAuth.OrganizationID, clusterUID string) error {
	return nil
}

type dexClusterAuthService struct {
	dexClient *dexClient
}

func NewDexClusterAuthService() (ClusterAuthService, error) {
	client, err := newDexClient(viper.GetString("auth.dexGrpcAddress"), viper.GetString("auth.dexGrpcCaCert"))
	if err != nil {
		return nil, err
	}

	return &dexClusterAuthService{dexClient: client}, nil
}

func (a *dexClusterAuthService) RegisterCluster(orgID pkgAuth.OrganizationID, clusterUID string) error {

	clientSecret, _ := secret.RandomString("randAlphaNum", 32)
	redirectURI := "http://127.0.0.1:1848/dex/cluster/callback"

	req := &api.CreateClientReq{
		Client: &api.Client{
			Id:           clusterUID,
			Name:         fmt.Sprint("Client for cluster", clusterUID),
			Secret:       clientSecret,
			RedirectUris: []string{redirectURI},
		},
	}

	if _, err := a.dexClient.CreateClient(context.TODO(), req); err != nil {
		return emperror.Wrapf(err, "failed to create dex client for cluster: %s", clusterUID)
	}

	// save the secret to the secret store
	secretRequest := secret.CreateSecretRequest{
		Type: pkgSecret.GenericSecret,
		Name: clusterUID + "-dex",
		Values: map[string]string{
			"clientID":     clusterUID,
			"clientSecret": clientSecret,
		},
	}

	_, err := secret.Store.Store(orgID, &secretRequest)

	if err != nil {
		return emperror.Wrapf(err, "failed to create secret for dex clientID/clientSecret for cluster: %s", clusterUID)
	}

	return nil
}

func (a *dexClusterAuthService) UnRegisterCluster(clusterUID string) error {

	req := &api.DeleteClientReq{
		Id: clusterUID,
	}

	if _, err := a.dexClient.DeleteClient(context.TODO(), req); err != nil {
		return emperror.Wrapf(err, "failed to delete dex client for cluster: %s", clusterUID)
	}

	return nil
}
