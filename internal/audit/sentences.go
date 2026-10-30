package audit

import (
	"fmt"
	"strings"
)

// Sentences shown in Activity. They follow the vocabulary in specs/UX.md.

func SentenceNetworkCreated(actor, name string) string {
	return fmt.Sprintf("%s set up the private network %q.", actor, name)
}

func SentenceDeviceAdded(actor, device string) string {
	return fmt.Sprintf("%s added %s.", actor, device)
}

func SentenceDeviceRemoved(_, device string) string {
	return fmt.Sprintf("%s was removed.", device)
}

func SentenceServerCheckedIn(server string) string {
	return fmt.Sprintf("%s checked in for the first time.", server)
}

func SentenceAdminSignedIn(actor string) string {
	return fmt.Sprintf("%s signed in.", actor)
}

func SentenceDeviceRefreshed(device string) string {
	return fmt.Sprintf("%s refreshed its security.", device)
}

func SentenceDeviceRefreshFailed(device string) string {
	return fmt.Sprintf("%s could not refresh its security and kept its old one.", device)
}

func SentenceNameKept(actor, name string) string {
	return fmt.Sprintf("%s asked Wardline to keep %s pointed at this computer.", actor, name)
}

func SentenceNameRemoved(actor, name string) string {
	return fmt.Sprintf("%s stopped Wardline keeping %s pointed at this computer.", actor, name)
}

func SentenceDeviceSetUp(device string) string {
	return fmt.Sprintf("%s was set up and is ready to connect.", device)
}

func SentenceReachChanged(actor string, on bool) string {
	if on {
		return fmt.Sprintf("%s turned on letting devices reach each other.", actor)
	}
	return fmt.Sprintf("%s turned off letting devices reach each other.", actor)
}

func SentenceDeviceRenamed(old, name string) string {
	return fmt.Sprintf("%s was renamed to %s.", old, name)
}

// SentenceDeviceRenamedOnNetwork adds what the machine is called on the network, when that changed with the rename.
func SentenceDeviceRenamedOnNetwork(old, name, short string) string {
	return fmt.Sprintf("%s was renamed to %s. Its name on the network is now %s.", old, name, short)
}

// SentenceDeviceNames says what a machine is called on the network after its names were changed by hand.
func SentenceDeviceNames(actor, device, short string, aliases []string) string {
	s := fmt.Sprintf("%s changed the names for %s. Its name on the network is %s", actor, device, short)
	if len(aliases) > 0 {
		s += " and it is also known as " + strings.Join(aliases, ", ")
	}
	return s + "."
}
