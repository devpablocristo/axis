package runtime

import "strings"

func tenantUserMemoryScopeID(orgID, userID string) string {
	orgID = strings.TrimSpace(orgID)
	userID = strings.TrimSpace(userID)
	if orgID == "" || userID == "" {
		return ""
	}
	return orgID + ":" + userID
}
