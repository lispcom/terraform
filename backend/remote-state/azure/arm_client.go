package azure

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/profiles/2017-03-09/resources/mgmt/resources"
	armStorage "github.com/Azure/azure-sdk-for-go/services/storage/mgmt/2016-05-01/storage"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/hashicorp/go-azure-helpers/authentication"
)

type ArmClient struct {
	// These Clients are only initialized if an Access Key isn't provided
	groupsClient          *resources.GroupsClient
	storageAccountsClient *armStorage.AccountsClient

	accessKey          string
	environment        azure.Environment
	resourceGroupName  string
	storageAccountName string
}

func buildArmClient(ctx context.Context, config BackendConfig) (*ArmClient, error) {
	env, err := authentication.DetermineEnvironment(config.Environment)
	if err != nil {
		return nil, err
	}
	client := ArmClient{
		environment:        *env,
		resourceGroupName:  config.ResourceGroupName,
		storageAccountName: config.StorageAccountName,
	}

	// if we have an Access Key - we don't need the other clients
	if config.AccessKey != "" {
		client.accessKey = config.AccessKey
		return &client, nil
	}

	builder := authentication.Builder{
		ClientID:       config.ClientID,
		ClientSecret:   config.ClientSecret,
		SubscriptionID: config.ClientSecret,
		TenantID:       config.TenantID,
		Environment:    config.Environment,

		// Feature Toggles
		SupportsClientSecretAuth: true,
		// TODO: support for Azure CLI / Client Certificate / MSI
	}
	armConfig, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("Error building ARM Config: %+v", err)
	}

	oauthConfig, err := adal.NewOAuthConfig(env.ActiveDirectoryEndpoint, config.TenantID)
	if err != nil {
		return nil, err
	}

	auth, err := armConfig.GetAuthorizationToken(oauthConfig, env.ResourceManagerEndpoint)
	if err != nil {
		return nil, err
	}

	accountsClient := armStorage.NewAccountsClientWithBaseURI(env.ResourceManagerEndpoint, config.SubscriptionID)
	client.configureClient(&accountsClient.Client, auth)
	client.storageAccountsClient = &accountsClient

	groupsClient := resources.NewGroupsClientWithBaseURI(env.ResourceManagerEndpoint, config.SubscriptionID)
	client.configureClient(&groupsClient.Client, auth)
	client.groupsClient = &groupsClient

	return &client, nil
}

func (c ArmClient) getBlobClient(ctx context.Context) (*storage.BlobStorageClient, error) {
	if c.accessKey != "" {
		storageClient, err := storage.NewBasicClientOnSovereignCloud(c.storageAccountName, c.accessKey, c.environment)
		if err != nil {
			return nil, fmt.Errorf("Error creating storage client for storage account %q: %s", c.storageAccountName, err)
		}
		client := storageClient.GetBlobService()
		return &client, nil
	}

	keys, err := c.storageAccountsClient.ListKeys(ctx, c.resourceGroupName, c.storageAccountName)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving keys for Storage Account %q: %s", c.storageAccountName, err)
	}

	if keys.Keys == nil {
		return nil, fmt.Errorf("Nil key returned for storage account %q", c.storageAccountName)
	}

	accessKeys := *keys.Keys
	accessKey := accessKeys[0].Value

	storageClient, err := storage.NewBasicClientOnSovereignCloud(c.storageAccountName, *accessKey, c.environment)
	if err != nil {
		return nil, fmt.Errorf("Error creating storage client for storage account %q: %s", c.storageAccountName, err)
	}
	client := storageClient.GetBlobService()
	return &client, nil
}

func (c *ArmClient) configureClient(client *autorest.Client, auth autorest.Authorizer) {
	// TODO: fixme
	//setUserAgent(client)
	client.Authorizer = auth
	client.Sender = buildSender()
	client.SkipResourceProviderRegistration = false
	client.PollingDuration = 60 * time.Minute
}
