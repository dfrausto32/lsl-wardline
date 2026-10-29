package audit

import "fmt"

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
