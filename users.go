package Cx1ClientGo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/go-querystring/query"
)

func (c *Cx1Client) GetCurrentUser() (User, error) {
	if c.user != nil {
		return *c.user, nil
	}
	var user User

	user, err := c.GetUserByID(c.claims.UserID)
	c.user = &user

	return *c.user, err
}

// this no longer works as of 2024-09-13 / version 3.21.5
func (c Cx1Client) Whoami() (WhoAmI, error) {
	var me WhoAmI
	response, err := c.sendRequestOther(http.MethodGet, "/auth/admin", "/console/whoami", nil, nil)
	if err != nil {
		return me, err
	}

	err = json.Unmarshal(response, &me)
	return me, err
}

// retrieves the first 'count' users
func (c Cx1Client) GetUsers(count uint64) ([]User, error) {
	c.logger.Debugf("Get %d Cx1 Users", count)

	_, users, err := c.GetXUsersFiltered(UserFilter{
		BaseIAMFilter:       BaseIAMFilter{Max: c.pagination.Users},
		BriefRepresentation: false,
	}, count)
	return users, err
}

func (c Cx1Client) GetAllUsers() ([]User, error) {
	c.logger.Debugf("Get all Cx1 Users")

	_, users, err := c.GetAllUsersFiltered(UserFilter{
		BaseIAMFilter:       BaseIAMFilter{Max: c.pagination.Users},
		BriefRepresentation: false,
	})
	return users, err
}

func (c Cx1Client) GetUserByID(userID string) (User, error) {
	c.logger.Debugf("Get Cx1 User by ID %v", userID)

	var user UserWithAttributes
	// Note: this list includes API Key/service account users from Cx1, remove the /admin/ for regular users only.
	response, err := c.sendRequestIAM(http.MethodGet, "/auth/admin", fmt.Sprintf("/users/%v?briefRepresentation=false", userID), nil, nil)
	if err != nil {
		return User{}, err
	}

	err = json.Unmarshal(response, &user)
	return toUser(&user), err
}

func (c Cx1Client) GetUserByUserName(username string) (User, error) {
	c.logger.Debugf("Get Cx1 User by Username: %v", username)

	_, users, err := c.GetAllUsersFiltered(UserFilter{
		BaseIAMFilter:       BaseIAMFilter{Max: c.pagination.Users},
		BriefRepresentation: false,
		Username:            username,
		Exact:               true,
	})

	if len(users) == 0 {
		return User{}, fmt.Errorf("no user %v found", username)
	}
	if len(users) > 1 {
		return User{}, fmt.Errorf("too many users (%d) match %v", len(users), username)
	}
	return users[0], err
}

func (c Cx1Client) GetUsersByUserName(username string) ([]User, error) {
	c.logger.Debugf("Get Cx1 Users matching search: %v", username)

	_, users, err := c.GetAllUsersFiltered(UserFilter{
		BaseIAMFilter:       BaseIAMFilter{Max: c.pagination.Users},
		BriefRepresentation: false,
		Username:            username,
		Exact:               false,
	})
	return users, err
}

func (c Cx1Client) GetUserByEmail(email string) (User, error) {
	c.logger.Debugf("Get Cx1 User by email: %v", email)
	_, users, err := c.GetAllUsersFiltered(UserFilter{
		BaseIAMFilter:       BaseIAMFilter{Max: c.pagination.Users},
		BriefRepresentation: false,
		Email:               email,
		Exact:               true,
	})

	if err != nil {
		return User{}, err
	}

	if len(users) == 0 {
		return User{}, fmt.Errorf("no user with email %v found", email)
	}

	return users[0], nil
}

func (c Cx1Client) GetUserCount() (uint64, error) {
	c.logger.Debugf("Get Cx1 User count")

	return c.GetUserCountFiltered(UserFilter{BaseIAMFilter: BaseIAMFilter{Max: 1}})
}

func (c Cx1Client) GetUserCountFiltered(filter UserFilter) (uint64, error) {
	params, _ := query.Values(filter)
	c.logger.Debugf("Get Cx1 User count with filter %v", params.Encode())

	response, err := c.sendRequestIAM(http.MethodGet, "/auth/admin", fmt.Sprintf("/users/count?%v", params.Encode()), nil, nil)
	if err != nil {
		return 0, err
	}

	count, err := strconv.ParseUint(string(response), 10, 64)
	return count, err
}

// Underlying function used by many GetUsers* calls
// Returns the number of applications matching the filter and the array of matching applications
func (c Cx1Client) GetUsersFiltered(filter UserFilter) ([]User, error) {
	var users []User
	var uwa []UserWithAttributes
	params, _ := query.Values(filter)
	if filter.Realm == "" {
		filter.Realm = c.tenant
	}

	response, err := c.sendRequestIAM(http.MethodGet, "/auth/admin", fmt.Sprintf("/users?%v", params.Encode()), nil, nil)
	if err != nil {
		return users, err
	}

	err = json.Unmarshal(response, &uwa)
	if err != nil {
		return users, err
	}

	users = toUsers(&uwa)

	return users, err
}

// returns all users matching the filter
func (c Cx1Client) GetAllUsersFiltered(filter UserFilter) (uint64, []User, error) {
	var users []User
	count, err := c.GetUserCountFiltered(filter)
	if err != nil {
		return count, users, err
	}
	return c.GetXUsersFiltered(filter, count)
}

// returns first X users matching the filter
func (c Cx1Client) GetXUsersFiltered(filter UserFilter, count uint64) (uint64, []User, error) {
	var users []User

	gs, err := c.GetUsersFiltered(filter)
	users = gs

	for err == nil && count > filter.Max+filter.First && filter.Max > 0 && uint64(len(users)) < count {
		filter.Bump()
		gs, err = c.GetUsersFiltered(filter)
		users = append(users, gs...)
	}

	if uint64(len(users)) > count {
		return count, users[:count], err
	}

	return count, users, err
}

func (c Cx1Client) CreateUser(newuser User) (User, error) {
	c.logger.Debugf("Creating a new user %v", newuser.String())
	newuser.UserID = ""
	jsonBody, err := json.Marshal(newuser)
	if err != nil {
		c.logger.Tracef("Failed to marshal data somehow: %s", err)
		return User{}, err
	}

	response, err := c.sendRequestRawIAM(http.MethodPost, "/auth/admin", "/users", bytes.NewReader(jsonBody), nil)
	if err != nil {
		return User{}, err
	}

	location := response.Header.Get("Location")
	if location != "" {
		lastInd := strings.LastIndex(location, "/")
		guid := location[lastInd+1:]
		c.logger.Tracef("New user ID: %v", guid)
		return c.GetUserByID(guid)
	} else {
		return User{}, fmt.Errorf("unknown error - no Location header redirect in response")
	}
}

/*
CreateSAMLUser will directly create a user that can log in via SAML, requiring the internal identifiers that are used within the identity provider.
This function requires some special behavior that's not supported by the standard user type, and requires a two-step process of creating and then updating the user.
*/
func (c Cx1Client) CreateSAMLUser(newuser User, idpAlias, idpUserId, idpUserName string) (User, error) {
	var samlUser User
	jsonData, err := json.Marshal(newuser)
	if err != nil {
		return samlUser, err
	}

	var userMap map[string]interface{}
	err = json.Unmarshal(jsonData, &userMap) // need to add properties to the submitted json
	if err != nil {
		return samlUser, err
	}

	userMap["totp"] = false
	fedId := make([]map[string]string, 1)
	fedId[0] = make(map[string]string)

	fedId[0]["identityProvider"] = idpAlias
	fedId[0]["userId"] = idpUserId
	fedId[0]["userName"] = idpUserName

	userMap["federatedIdentities"] = fedId

	jsonData, _ = json.Marshal(userMap)

	response, err := c.sendRequestRawIAM(http.MethodPost, "/auth/admin", "/users", bytes.NewReader(jsonData), nil)
	if err != nil {
		return samlUser, err
	}

	location := response.Header.Get("Location")
	if location == "" {
		return samlUser, fmt.Errorf("unknown error - no Location header redirect in response")
	}

	lastInd := strings.LastIndex(location, "/")
	guid := location[lastInd+1:]
	c.logger.Tracef(" New SAML user ID: %v", guid)
	response2, err := c.sendRequestIAM(http.MethodGet, "/auth/admin", fmt.Sprintf("/users/%v", guid), nil, nil)
	if err != nil {
		return samlUser, err
	}

	err = json.Unmarshal(response2, &userMap)
	if err != nil {
		return samlUser, err
	}

	userMap["requiredActions"] = []string{}

	jsonData, _ = json.Marshal(userMap)
	_, err = c.sendRequestIAM(http.MethodPut, "/auth/admin", fmt.Sprintf("/users/%v", guid), bytes.NewReader(jsonData), nil)

	if err != nil {
		return samlUser, err
	}

	return c.GetUserByID(guid)
}

func (c Cx1Client) UpdateUser(user *User) error {
	c.logger.Debugf("Updating user %v", user.String())
	jsonBody, err := json.Marshal(user)
	if err != nil {
		c.logger.Tracef("Failed to marshal data somehow: %s", err)
		return err
	}

	_, err = c.sendRequestIAM(http.MethodPut, "/auth/admin", fmt.Sprintf("/users/%v", user.UserID), bytes.NewReader(jsonBody), nil)
	return err
}

func (c Cx1Client) DeleteUser(user *User) error {
	return c.DeleteUserByID(user.UserID)
}

func (c Cx1Client) DeleteUserByID(userid string) error {
	c.logger.Debugf("Deleting a user %v", userid)

	_, err := c.sendRequestIAM(http.MethodDelete, "/auth/admin", fmt.Sprintf("/users/%v", userid), nil, nil)
	if err != nil {
		c.logger.Tracef("Failed to delete user: %s", err)
		return err
	}
	return nil
}

func (c Cx1Client) UserLink(u *User) string {
	return fmt.Sprintf("%v/auth/admin/%v/console/#/realms/%v/users/%v", c.iamUrl, c.tenant, c.tenant, u.UserID)
}

func (c Cx1Client) UserIsTenantOwner(u *User) (bool, error) {
	owner, err := c.GetTenantOwner()
	if err != nil {
		return false, err
	}

	return (u.UserID == owner.UserID), nil
}

func (c Cx1Client) GetUserGroups(user *User) ([]Group, error) {
	var usergroups []Group

	response, err := c.sendRequestIAM(http.MethodGet, "/auth/admin", fmt.Sprintf("/users/%v/groups", user.UserID), nil, nil)

	if err != nil {
		c.logger.Tracef("Failed to fetch user's groups: %s", err)
		return []Group{}, err
	}

	err = json.Unmarshal(response, &usergroups)
	if err != nil {
		c.logger.Tracef("Failed to unmarshal response: %s", err)
		return []Group{}, err
	}

	user.Groups = usergroups
	user.FilledGroups = true

	return user.Groups, nil
}

func (c Cx1Client) AssignUserToGroupByID(user *User, groupId string) error {
	inGroup, err := user.IsInGroupByID(groupId)
	if err != nil {
		return err
	}

	if !inGroup {
		params := map[string]string{
			"realm":   c.tenant,
			"userId":  user.UserID,
			"groupId": groupId,
		}

		jsonBody, err := json.Marshal(params)
		if err != nil {
			c.logger.Tracef("Failed to marshal group params: %s", err)
			return err
		}

		_, err = c.sendRequestIAM(http.MethodPut, "/auth/admin", fmt.Sprintf("/users/%v/groups/%v", user.UserID, groupId), bytes.NewReader(jsonBody), nil)
		if err != nil {
			c.logger.Tracef("Failed to add user to group: %s", err)
			return err
		}

		// TODO: Should user structure be updated to include the new group membership? (get group obj, append to list)
		group, err := c.GetGroupByID(groupId)
		if err != nil {
			c.logger.Tracef("Failed to get group info for %v: %s", groupId, err)
			return err
		}
		user.Groups = append(user.Groups, group)
	}
	return nil
}

func (c Cx1Client) RemoveUserFromGroupByID(user *User, groupId string) error {
	inGroup, err := user.IsInGroupByID(groupId)
	if err != nil {
		return err
	}

	if inGroup {
		params := map[string]string{
			"realm":   c.tenant,
			"userId":  user.UserID,
			"groupId": groupId,
		}

		jsonBody, err := json.Marshal(params)
		if err != nil {
			c.logger.Tracef("Failed to marshal group params: %s", err)
			return err
		}

		_, err = c.sendRequestIAM(http.MethodDelete, "/auth/admin", fmt.Sprintf("/users/%v/groups/%v", user.UserID, groupId), bytes.NewReader(jsonBody), nil)
		if err != nil {
			c.logger.Tracef("Failed to remove user from group: %s", err)
			return err
		}

		index := -1
		for id, g := range user.Groups {
			if g.GroupID == groupId {
				index = id
				break
			}
		}

		if index != -1 {
			user.Groups = RemoveGroup(user.Groups, index)
		}
	}
	return nil
}

// New generic functions for roles for convenience
func (c Cx1Client) GetUserRoles(user *User) ([]Role, error) {
	appRoles, err := c.getUserRolesByClientID(user.UserID, c.GetASTAppID())
	if err != nil {
		return []Role{}, nil
	}

	iamRoles, err := c.GetUserIAMRoles(user)
	if err != nil {
		return []Role{}, nil
	}

	user.Roles = append(appRoles, iamRoles...)
	user.FilledRoles = true

	return user.Roles, nil
}

func (c Cx1Client) AddUserRoles(user *User, roles *[]Role) error {
	appRoles := []Role{}
	iamRoles := []Role{}

	for _, r := range *roles {
		if r.ClientID == c.GetASTAppID() {
			appRoles = append(appRoles, r)
		} else if r.ClientID == c.GetTenantID() {
			iamRoles = append(iamRoles, r)
		} else {
			c.logger.Errorf("Request to add role to unhandled client ID: %v", r.String())
		}
	}

	if len(appRoles) > 0 {
		err := c.AddUserAppRoles(user, &appRoles)
		if err != nil {
			return fmt.Errorf("failed to add application roles: %s", err)
		} else {
			user.Roles = append(user.Roles, appRoles...)
		}
	}

	if len(iamRoles) > 0 {
		err := c.AddUserIAMRoles(user, &iamRoles)
		if err != nil {
			return fmt.Errorf("failed to add IAM roles: %s", err)
		} else {
			user.Roles = append(user.Roles, iamRoles...)
		}
	}

	return nil
}

func (c Cx1Client) RemoveUserRoles(user *User, roles *[]Role) error {
	appRoles := []Role{} // roles to remove
	iamRoles := []Role{} // roles to remove
	remainingRoles := []Role{}

	for _, r := range user.Roles {
		if r.ClientID == c.GetASTAppID() {
			matched := false
			for _, ar := range *roles { // is this user's app-role to be removed
				if ar.RoleID == r.RoleID { // yes, remove it
					appRoles = append(appRoles, r)
					matched = true
					break
				}
			}
			if !matched { // not removing this role, keep it as remaining
				remainingRoles = append(remainingRoles, r)
			}
		} else if r.ClientID == c.GetTenantID() {
			matched := false
			for _, ir := range *roles { // is this user's app-role to be removed
				if ir.RoleID == r.RoleID { // yes, remove it
					iamRoles = append(iamRoles, r)
					matched = true
					break
				}
			}
			if !matched { // not removing this role, keep it as remaining
				remainingRoles = append(remainingRoles, r)
			}
		} else {
			c.logger.Errorf("Request to remove role from unhandled client ID: %v", r.String())
		}
	}

	if len(appRoles) > 0 {
		err := c.RemoveUserAppRoles(user, &appRoles)
		if err != nil {
			return fmt.Errorf("failed to remove application roles: %s", err)
		}
	}

	if len(iamRoles) > 0 {
		err := c.RemoveUserIAMRoles(user, &iamRoles)
		if err != nil {
			return fmt.Errorf("failed to remove IAM roles: %s", err)
		}
	}

	user.Roles = remainingRoles

	return nil
}

// more specific functions related to role management.
/*
	In cx1 there are two different types of roles: Application roles and IAM roles

	IAM roles have permissions related to user management - and access control in general - with functionality provided by KeyCloak.
		In KeyCloak terms, these roles are scoped to your tenant Realm. Thus we have the "*UserIAMRoles" functions.
	Application roles have permissions related to the CheckmarxOne product functionality such as creating projects, starting scans, and generating reports.
		In KeyCloak terms, the CheckmarxOne application is a Client within your tenant Realm. Thus we have the "*UserAppRoles" functions.

*/

func (c Cx1Client) GetUserAppRoles(user *User) ([]Role, error) {
	return c.getUserRolesByClientID(user.UserID, c.GetASTAppID())
}
func (c Cx1Client) AddUserAppRoles(user *User, roles *[]Role) error {
	return c.addUserRolesByClientID(user.UserID, c.GetASTAppID(), roles)
}
func (c Cx1Client) RemoveUserAppRoles(user *User, roles *[]Role) error {
	return c.removeUserRolesByClientID(user.UserID, c.GetASTAppID(), roles)
}

func (c Cx1Client) GetUserIAMRoles(user *User) ([]Role, error) {
	return c.getUserKCRoles(user.UserID)
}
func (c Cx1Client) AddUserIAMRoles(user *User, roles *[]Role) error {
	return c.addUserKCRoles(user.UserID, roles)
}
func (c Cx1Client) RemoveUserIAMRoles(user *User, roles *[]Role) error {
	return c.removeUserKCRoles(user.UserID, roles)
}

func (c Cx1Client) getUserRolesByClientID(userID string, clientID string) ([]Role, error) {
	c.logger.Debugf("Get Cx1 Rolemappings for userid %v and clientid %v", userID, clientID)

	var roles []Role
	response, err := c.sendRequestIAM(http.MethodGet, "/auth/admin", fmt.Sprintf("/users/%v/role-mappings/clients/%v", userID, clientID), nil, nil)
	if err != nil {
		return roles, err
	}
	err = json.Unmarshal(response, &roles)
	return roles, err
}
func (c Cx1Client) addUserRolesByClientID(userID string, clientID string, roles *[]Role) error {
	c.logger.Debugf("Add Cx1 Rolemappings for userid %v and clientid %v", userID, clientID)

	jsonBody, err := json.Marshal(roles)
	if err != nil {
		c.logger.Tracef("Failed to marshal roles: %s", err)
		return err
	}

	_, err = c.sendRequestIAM(http.MethodPost, "/auth/admin", fmt.Sprintf("/users/%v/role-mappings/clients/%v", userID, clientID), bytes.NewReader(jsonBody), nil)
	return err
}
func (c Cx1Client) removeUserRolesByClientID(userID string, clientID string, roles *[]Role) error {
	c.logger.Debugf("Add Cx1 Rolemappings for userid %v and clientid %v", userID, clientID)

	jsonBody, err := json.Marshal(roles)
	if err != nil {
		c.logger.Tracef("Failed to marshal roles: %s", err)
		return err
	}

	_, err = c.sendRequestIAM(http.MethodDelete, "/auth/admin", fmt.Sprintf("/users/%v/role-mappings/clients/%v", userID, clientID), bytes.NewReader(jsonBody), nil)
	return err
}

func (c Cx1Client) getUserKCRoles(userID string) ([]Role, error) {
	c.logger.Debugf("Get Cx1 Tenant realm Rolemappings for userid %v", userID)

	var roles []Role
	response, err := c.sendRequestIAM(http.MethodGet, "/auth/admin", fmt.Sprintf("/users/%v/role-mappings/realm", userID), nil, nil)
	if err != nil {
		return roles, err
	}
	err = json.Unmarshal(response, &roles)
	return roles, err
}
func (c Cx1Client) addUserKCRoles(userID string, roles *[]Role) error {
	c.logger.Debugf("Add Cx1 Tenant realm Rolemappings for userid %v", userID)

	jsonBody, err := json.Marshal(roles)
	if err != nil {
		c.logger.Tracef("Failed to marshal roles: %s", err)
		return err
	}

	_, err = c.sendRequestIAM(http.MethodPost, "/auth/admin", fmt.Sprintf("/users/%v/role-mappings/realm", userID), bytes.NewReader(jsonBody), nil)
	return err
}
func (c Cx1Client) removeUserKCRoles(userID string, roles *[]Role) error {
	c.logger.Debugf("Add Cx1 Tenant realm Rolemappings for userid %v", userID)

	jsonBody, err := json.Marshal(roles)
	if err != nil {
		c.logger.Tracef("Failed to marshal roles: %s", err)
		return err
	}

	_, err = c.sendRequestIAM(http.MethodDelete, "/auth/admin", fmt.Sprintf("/users/%v/role-mappings/realm", userID), bytes.NewReader(jsonBody), nil)
	return err
}

// utility
func toUser(u *UserWithAttributes) User {
	user := u.User
	if len(u.Attributes.LastLogin) > 0 {
		user.LastLogin = u.Attributes.LastLogin[0]
	}
	return user
}

func toUsers(users *[]UserWithAttributes) []User {
	ret := []User{}
	for _, u := range *users {
		user := u.User
		if len(u.Attributes.LastLogin) > 0 {
			user.LastLogin = u.Attributes.LastLogin[0]
		}
		ret = append(ret, user)
	}
	return ret
}
