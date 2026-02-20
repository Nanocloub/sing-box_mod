package mieru

import (
	"net"

	mieruappctlcommon "github.com/enfein/mieru/v3/pkg/appctl/appctlcommon"
	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

func (h *Inbound) AddUsers(users []option.MieruUser) error {
	if len(users) == 0 {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Create a new slice to avoid sharing underlying array with h.options.Users
	allUsers := make([]option.MieruUser, 0, len(h.options.Users)+len(users))
	allUsers = append(allUsers, h.options.Users...)
	allUsers = append(allUsers, users...)

	// Deduplicate while preserving order (like naive protocol)
	userSeen := make(map[string]struct{}, len(allUsers))
	newUsers := make([]option.MieruUser, 0, len(allUsers))
	for _, u := range allUsers {
		if _, exists := userSeen[u.Name]; exists {
			continue
		}
		userSeen[u.Name] = struct{}{}
		newUsers = append(newUsers, u)
	}

	// Update users dynamically using mux.SetServerUsers()
	if err := h.updateServerUsers(newUsers); err != nil {
		return err
	}

	h.options.Users = newUsers
	h.updateUserNames(newUsers)
	return nil
}

func (h *Inbound) DelUsers(names []string) error {
	if len(names) == 0 {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	toDelete := make(map[string]struct{}, len(names))
	for _, name := range names {
		toDelete[name] = struct{}{}
	}

	// Filter out deleted users
	remaining := make([]option.MieruUser, 0, len(h.options.Users))
	for _, user := range h.options.Users {
		if _, found := toDelete[user.Name]; !found {
			remaining = append(remaining, user)
		}
	}

	// Early return if no users were actually deleted
	if len(remaining) == len(h.options.Users) {
		return nil
	}

	// Update users dynamically using mux.SetServerUsers()
	if err := h.updateServerUsers(remaining); err != nil {
		return err
	}

	h.options.Users = remaining
	h.updateUserNames(remaining)

	// Close existing connections for deleted users
	h.userconns.Range(func(key, value interface{}) bool {
		userName := value.(string)
		if _, found := toDelete[userName]; found {
			if conn, ok := key.(net.Conn); ok {
				conn.Close()
			}
			h.userconns.Delete(key)
		}
		return true
	})

	return nil
}

// updateServerUsers updates the user list using mieru's internal mux.SetServerUsers() method.
// This allows dynamic user updates without rebuilding the entire config or calling Store().
func (h *Inbound) updateServerUsers(users []option.MieruUser) error {
	// Check if mux is initialized (should be set during Start())
	if h.mux == nil {
		return E.New("mux is not initialized, server may not be started")
	}

	// Pre-allocate slice with exact capacity to avoid reallocations
	pbUsers := make([]*mierupb.User, len(users))
	for i := range users {
		// Take address of struct fields directly from the slice
		// Each iteration the range variable is updated, so taking address is safe
		pbUsers[i] = &mierupb.User{
			Name:     &users[i].Name,
			Password: &users[i].Password,
		}
	}

	// Convert user list to map as required by SetServerUsers
	userMap := mieruappctlcommon.UserListToMap(pbUsers)

	// Update users in the mux (SetServerUsers is thread-safe)
	h.mux.SetServerUsers(userMap)

	return nil
}

// updateUserNames updates the cached userNames list.
func (h *Inbound) updateUserNames(users []option.MieruUser) {
	h.userNames = make([]string, len(users))
	for i, user := range users {
		h.userNames[i] = user.Name
	}
}
