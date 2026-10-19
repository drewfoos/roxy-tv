package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Legal text (Terms of Use, Privacy) is served by the server so every device and profile
// sees the same version, and acceptance is recorded per user (the login, not the
// profile: the household shares it). The text is versioned: a changed version is shown
// again at the next launch.

// TermsVersion bumps whenever the wording changes in a way people should see again.
const TermsVersion = "2026-10-20b"

var legalDocs = map[string]struct {
	Title string
	Body  string
}{
	"terms":   {"Terms of Use", termsText},
	"privacy": {"Privacy", privacyText},
}

// GET /v1/legal/{doc} (terms | privacy) -> {doc, title, version, body}
func (s *Server) legalDoc(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(pathTail(r.URL.Path))
	d, ok := legalDocs[name]
	if !ok {
		writeError(w, 404, "unknown document")
		return
	}
	writeJSON(w, 200, map[string]any{"doc": name, "title": d.Title, "version": TermsVersion, "body": d.Body})
}

// GET /v1/legal/status -> {version, accepted, acceptedVersion}
func (s *Server) legalStatus(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	v, _ := s.store.AcceptedTerms(r.Context(), p.UserID)
	writeJSON(w, 200, map[string]any{"version": TermsVersion, "accepted": v == TermsVersion, "acceptedVersion": v})
}

// POST /v1/legal/accept {version} -> 204. Records that this login accepted the current terms.
func (s *Server) legalAccept(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body)
	if body.Version != TermsVersion {
		writeError(w, 409, "terms version mismatch; reload the terms")
		return
	}
	if err := s.store.SetAcceptedTerms(r.Context(), p.UserID, TermsVersion); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

func pathTail(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

const termsText = `Roxy TV is self-hosted software. It runs on a server you (or someone in your household) operate, and it plays media stored on that server. It is provided as is, without warranty of any kind.

**Your media, your responsibility.** Roxy does not provide, host, index, or distribute any film, series, music, or other content. Everything the app shows comes from files on your own server and from public metadata services (TMDB, AniList). You are responsible for making sure you have the right to possess and watch the media in your library under the laws that apply to you.

**Requests and the arr apps.** Roxy can be connected to Sonarr and Radarr, which the server's administrator installs and configures separately. When connected, a request made in Roxy is passed to those applications; what they do with it is governed by their own configuration, indexers, and download clients, none of which Roxy controls. Use these features only to obtain content you are legally entitled to. The authors of Roxy are not responsible for anything downloaded, shared, or stored as a result of a request.

**Third-party services.** Metadata, artwork, and character images come from The Movie Database (TMDB) and AniList under their terms. This product uses the TMDB API but is not endorsed or certified by TMDB. Opening a link on a details page takes you to the third party's site.

**Accounts and profiles.** Your server administrator creates accounts. Profiles under one account share that account's access and these terms: agreeing when you sign in covers every profile on the login. The administrator can see what the household has requested and watched. Keep your password private.

**Offline downloads.** Downloaded episodes are copies of your server's files, stored on this device for your own offline viewing. They are deleted when you remove them or uninstall the app.

**No liability.** To the fullest extent permitted by law, the authors and contributors of Roxy TV are not liable for any loss, damage, or legal consequence arising from your use of the software or of any content obtained with it.

**Changes.** These terms may change as the software evolves. You will be asked to read them again when they do.`

const privacyText = `Roxy TV is self-hosted. There is no Roxy cloud and no analytics. What follows is everything the software does with data about you.

**What stays on your server.** Your account, profiles, watch history, resume positions, favourites, requests, and the settings you change are stored in the Roxy Server database on your own server. They never leave it except to reach your devices.

**What the app keeps on your device.** Your login token, the chosen profile, player settings, any episodes you download, and a copy of the profile list so "Who's watching?" works away from home. Nothing else. There are no advertising identifiers and no tracking.

**Third-party services the server contacts.** The Movie Database (TMDB) and AniList, to fetch titles, artwork, episode lists, and character images. Those requests carry the title being looked up, not your identity. Sonarr and Radarr, when the administrator connects them, receive the titles you request. Your server's administrator can see all of this.

**Third-party services your device contacts.** Posters, stills, and cast photos on the Discover tab and details pages load straight from TMDB's image servers, so TMDB sees your device's address as any website would. Links on a details page open the third party's site in your browser. Everything else goes through your own server.

**App updates.** When your server hosts a newer build, the app offers it; the file comes from your server, not from the internet.

**Crash and debug logs.** The app writes logs to the device's standard log buffer during development builds. They are not sent anywhere.

**Your administrator.** Because the server is run by a person you know, that person can see the data above. Talk to them about retention or deletion; the software offers no cloud account to close.`
