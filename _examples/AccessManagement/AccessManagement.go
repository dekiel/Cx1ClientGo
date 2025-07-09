package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/cxpsemea/Cx1ClientGo"
	"github.com/sirupsen/logrus"
	easy "github.com/t-tomalak/logrus-easy-formatter"
)

func main() {
	logger := logrus.New()
	logger.SetLevel(logrus.TraceLevel)
	myformatter := &easy.Formatter{}
	myformatter.TimestampFormat = "2006-01-02 15:04:05.000"
	myformatter.LogFormat = "[%lvl%][%time%] %msg%\n"
	logger.SetFormatter(myformatter)
	logger.SetOutput(os.Stdout)

	if len(os.Args) < 5 {
		log.Fatalf("Usage: go run . <cx1 url> <iam url> <tenant> <api key>")
	}

	logger.Info("Starting")

	base_url := os.Args[1]
	iam_url := os.Args[2]
	tenant := os.Args[3]
	api_key := os.Args[4]

	//proxyURL, _ := url.Parse("http://127.0.0.1:8080")
	//transport := &http.Transport{}
	//transport.Proxy = http.ProxyURL(proxyURL)
	//transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	httpClient := &http.Client{}
	//httpClient.Transport = transport

	cx1client, err := Cx1ClientGo.NewAPIKeyClient(httpClient, base_url, iam_url, tenant, api_key, logger)
	if err != nil {
		logger.Fatalf("Error creating client: %s", err)
	}

	checkCurrentUserAccess(cx1client, logger)

	testclient, testuser, err := createOIDCClient(cx1client, logger)
	if err != nil {
		logger.Fatalf("Failed to get or create OIDC Client: %s", err)
	}

	err = addAccessAssignments(cx1client, testuser, tenant, logger)
	if err != nil {
		logger.Errorf("Failed to add user assignment for cx1clientgo_test service user: %s", err)
	} else {
		logger.Info("Testing new OIDC Client by logging in as cx1clientgo_test")
		testcx1client, err := Cx1ClientGo.NewOAuthClient(httpClient, base_url, iam_url, tenant, testclient.ClientID, testclient.ClientSecret, logger)
		if err != nil {
			logger.Errorf("Failed to log in as 'cx1clientgo_test' OIDC Client: %s", err)
		} else {
			checkCurrentUserAccess(testcx1client, logger)
		}
	}

	err = cx1client.DeleteClientByID(testclient.ID)
	if err != nil {
		logger.Fatalf("Failed to delete oidc client 'cx1clientgo_test': %s", err)
	}

}

func checkCurrentUserAccess(cx1client *Cx1ClientGo.Cx1Client, logger *logrus.Logger) {
	currentuser, err := cx1client.Whoami()
	if err != nil {
		logger.Errorf("Failed to get current user identity: %s", err)
	}

	logger.Infof("Currently logged in as: %v", currentuser.String())

	allAccess, accessibleResources, err := cx1client.CheckAccessibleResources([]string{"tenant", "project", "application"}, "ast-scanner")
	if err != nil {
		logger.Errorf("Failed to check current user access assignments: %s", err)
		return
	}

	logger.Infof("Current user has access to all: %t", allAccess)
	logger.Infof("Current user has the following resources accessible: ")
	for _, a := range accessibleResources {
		logger.Infof(" - %v %v: %v", a.ResourceType, a.ResourceID, strings.Join(a.Roles, ","))
	}

	tenantId := cx1client.GetTenantID()
	hasAccess, err := cx1client.CheckAccessToResourceByID(tenantId, "tenant", "ast-scanner")
	if err != nil {
		logger.Errorf("Failed to check current user's access to tenant %v: %s", tenantId, err)
		return
	}

	logger.Infof("Current user has ast-scanner access to tenant %v: %t", tenantId, hasAccess)
}

func createOIDCClient(cx1client *Cx1ClientGo.Cx1Client, logger *logrus.Logger) (Cx1ClientGo.OIDCClient, Cx1ClientGo.User, error) {
	testclient, err := cx1client.GetClientByName("cx1clientgo_test")
	var user Cx1ClientGo.User
	if err != nil {
		logger.Warnf("Failed to find existing OIDC Client 'cx1clientgo_test' - trying to create this OIDC client. Error: %s", err)
		testclient, err = cx1client.CreateClient("cx1clientgo_test", []string{ /*no email for notification*/ }, 30)
		if err != nil {
			return testclient, user, fmt.Errorf("failed to create oidc client 'cx1clientgo_test': %s", err)
		}
	}

	user, err = cx1client.GetServiceAccountByID(testclient.ID)
	if err != nil {
		return testclient, user, fmt.Errorf("failed to get service account for oidc client 'cx1clientgo_test': %s", err)
	}

	logger.Infof("cx1clientgo_test oidc client service account user is: %v", user.String())

	scanner_role, err := cx1client.GetRoleByName("ast-scanner")
	if err != nil {
		return testclient, user, fmt.Errorf("failed to find 'ast-scanner' role: %s", err)
	}

	err = cx1client.AddUserRoles(&user, &[]Cx1ClientGo.Role{scanner_role})
	if err != nil {
		return testclient, user, fmt.Errorf("failed to add 'ast-scanner' role to user: %s", err)
	}
	return testclient, user, nil
}

func addAccessAssignments(cx1client *Cx1ClientGo.Cx1Client, user Cx1ClientGo.User, tenant string, logger *logrus.Logger) error {
	tenantId := cx1client.GetTenantID()
	access := Cx1ClientGo.AccessAssignment{
		TenantID:     tenantId,
		ResourceID:   tenantId,
		ResourceType: "tenant",
		ResourceName: tenant,
		EntityRoles:  []Cx1ClientGo.AccessAssignedRole{{Name: "ast-scanner"}},
		EntityID:     user.UserID,
		EntityType:   "user",
		EntityName:   "cx1clientgo_test",
	}

	err := cx1client.AddAccessAssignment(access)
	if err != nil {
		return fmt.Errorf("failed to assign access: %s", err)
	}

	accessAssignment, err := cx1client.GetEntitiesAccessToResourceByID(tenantId, "tenant")
	if err != nil {
		return fmt.Errorf("failed to get entities with access to tenant: %s", err)
	}

	logger.Info("The following access assignments exist for the cx1clientgo_test OIDC Client on tenant:")
	for _, a := range accessAssignment {
		entityRoleStr := []string{}
		for _, r := range a.EntityRoles {
			entityRoleStr = append(entityRoleStr, r.Name)
		}
		logger.Infof(" - Entity %v has roles %v", a.EntityID, strings.Join(entityRoleStr, ", "))
	}
	return nil
}
