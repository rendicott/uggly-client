package main

import (
	"context"
	"encoding/json"
	"fmt"
	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggsec"
	"google.golang.org/grpc/metadata"
	"time"
)

func (b *ugglyBrowser) loadCookies() (err error) {
	contents, err := b.readVault()
	if err != nil {
		return err
	}
	cookieJar := []*serverCookies{}
	err = json.Unmarshal([]byte(contents), &cookieJar)
	if err != nil {
		return err
	}
	for _, jarCookie := range cookieJar {
		b.cookies[jarCookie.Server] = jarCookie.Cookies
	}
	loggo.Info("successfully loaded cookies from vault", "num_cookies", len(b.cookies))
	return err
}

type serverCookies struct {
	Server  string
	Cookies []*pb.Cookie
}

func (b *ugglyBrowser) loadVault() (vault *uggsec.Vault, err error) {
	params := uggsec.VaultInput{
		Filename: *b.settings.VaultFile,
		Service:  "ugglyc",
		User:     "browser",
	}
	vault, err = uggsec.InitSmart(&params)
	if err != nil {
		loggo.Error("error initiating keyring", "error", err.Error())
		loggo.Info("using ENV var instead")
		// must contain 32 byte string. Defaults to UGGSECP if left blank
		params.PasswordEnvVar = *b.settings.VaultPassEnvVar
		vault, err = uggsec.InitSmart(&params)
		if err != nil {
			return vault, err
		}
	}
	return vault, err
}

func (b *ugglyBrowser) writeVault(contents string) (err error) {
	vault, err := b.loadVault()
	if err != nil {
		return err
	}
	return vault.Write(contents)
}

func (b *ugglyBrowser) readVault() (contents string, err error) {
	vault, err := b.loadVault()
	if err != nil {
		return contents, err
	}
	return vault.Read()
}

func (b *ugglyBrowser) countCookies() (cookieCount int) {
	for _, cookies := range b.cookies {
		cookieCount += len(cookies)
	}
	return cookieCount
}

func (b *ugglyBrowser) clearSessionCookies() {
	loggo.Debug("before clearing session cookies", "num_cookies", b.countCookies())
	now := time.Now()
	newCookieMap := make(map[string][]*pb.Cookie, 0)
	for server, cookies := range b.cookies {
		loggo.Debug("clearSessionCookies(), processing cookies for server",
			"server", server)
		storeCookies := []*pb.Cookie{}
		for _, cookie := range cookies {
			loggo.Debug("checking cookie", "cookie.Key", cookie.Key)
			// check expiration
			if cookie.Expires == "" {
				loggo.Debug("throwing out cookie because Expired is blank",
					"cookie.Key", cookie.Key)
				// means it's a session cookie and will be thrown out
				continue
			} else if cookie.Expires != "" {
				expires, err := time.Parse(time.RFC1123, cookie.Expires)
				if err != nil {
					loggo.Debug("throwing out cookie because can't parse Expired",
						"cookie.Key", cookie.Key)
					// can't tell so we throw out anyway
					continue
				}
				if now.After(expires) {
					loggo.Debug("throwing out cookie because Expired",
						"cookie.Key", cookie.Key)
					continue
				} else {
					loggo.Debug("adding cookie to storeCookies",
						"cookie.Key", cookie.Key)
					storeCookies = append(storeCookies, cookie)
				}

			}
		}
		if len(storeCookies) > 0 {
			newCookieMap[server] = storeCookies
		}
	}
	b.cookies = newCookieMap
	loggo.Debug("after clearing session cookies", "num_cookies", b.countCookies())
}

func (b *ugglyBrowser) storeCookies() (err error) {
	// clear non permanent cookies before storing
	b.clearSessionCookies()
	cookieJar := []*serverCookies{}
	for server, cookies := range b.cookies {
		cookieJar = append(cookieJar, &serverCookies{
			Server:  server,
			Cookies: cookies,
		})
		msg := fmt.Sprintf("jarred %d cookies for server '%s'", len(cookies), server)
		loggo.Debug(msg)
	}
	loggo.Info("jarred cookies for servers", "num_servers", len(cookieJar))
	dat, err := json.Marshal(cookieJar)
	if err != nil {
		return err
	}
	//err = ioutil.WriteFile("unencrypted-cookies.json", dat, 0755)
	//if err != nil {
	//	return err
	//}
	err = b.writeVault(string(dat))
	if err == nil {
		loggo.Info("successfully stored cookies to disk")
	}
	return err
}

// takes a PageRequest and integrates any eligible cookies from the browser's cache
// into the request and returns the cookie'fied request as a response
func (b *ugglyBrowser) addCookies(
	ctx context.Context, pq *pb.PageRequest) (context.Context, *pb.PageRequest) {
	destServer := pq.Server
	crossSiteCookies := []*pb.Cookie{}
	// find any cookies set by other servers that may need to be sent to this destination
	for serverCache, serverCookies := range b.cookies {
		for _, cookie := range serverCookies {
			// make sure we're looking at true cross site cookies
			if cookie.Server == destServer && serverCache != destServer {
				// check to see if we're allowed to send
				// SameSite 0 = Strict
				// SameSite 1 = None
				if cookie.SameSite == 1 && cookie.Secure {
					crossSiteCookies = append(crossSiteCookies, cookie)
				} else {
					loggo.Debug("cookie not allowed to be sent cross site")
				}
			}
		}
	}
	var allPotentialCookies []*pb.Cookie
	allPotentialCookies = append(allPotentialCookies, b.cookies[destServer]...)
	allPotentialCookies = append(allPotentialCookies, crossSiteCookies...)
	loggo.Info("processing potential cookies", "potentialCookies", len(allPotentialCookies))
	finalCookies := []*pb.Cookie{}
	// now we go through this server's cached cookies combined with cross site and process further
	now := time.Now()
	for _, potentialCookie := range allPotentialCookies {
		if potentialCookie.Private {
			loggo.Debug("discarding cookie because it's marked private")
			continue
		}
		// check if cookie is expired
		expires, err := time.Parse(time.RFC1123, potentialCookie.Expires)
		// if this errors then we just assume it's a session cookie since
		// we can't validate if it's expired or not. Therefore it has no
		// bearing on the logic we would use to exclude it based on expiry
		if err == nil {
			// means we can actually compare against our local date and exclude if expired
			if now.After(expires) {
				loggo.Debug("discarding cookie because it's expired")
				continue
			}
		}
		if potentialCookie.SameSite == pb.Cookie_STRICT && potentialCookie.Server != destServer {
			// means this server is not the server that set the cookie
			// and since sameSite is Strict then we need to discard
			loggo.Debug("discarding cookie because samesite set to strict")
			continue
		}
		if potentialCookie.SameSite == pb.Cookie_NONE && !pq.Secure {
			// means cookie's samesite property is set to Lax but since
			// the request is not secure we can't send the cookie
			loggo.Debug("discarding cookie as samesite == lax and request is not secure")
			continue
		}
		if potentialCookie.Page != "" {
			if potentialCookie.Page != pq.Name {
				// means the page restriction was set in the cookie and the
				// page we're requesting is not a match
				loggo.Debug("discarding cookie as requested page != cookie page")
				continue
			}
		}
		// if it passed the gauntlet then it goes into final cookies
		finalCookies = append(finalCookies, potentialCookie)
	}
	// now prep the final cookies for sending and append them to pagerequest
	for _, cookie := range finalCookies {
		if cookie.Secure && !pq.Secure {
			loggo.Debug("discarding secure cookie on unsecure connection")
			continue
		}
		// check to see if we need to put the cookie in metadata
		if cookie.Metadata {
			// inject metadata into ctx
			loggo.Debug("adding cookie to metadata", "cookie.Key", cookie.Key)
			ctx = metadata.AppendToOutgoingContext(ctx, cookie.Key, cookie.Value)
		}
		pq.SendCookies = append(pq.SendCookies, cookie)
	}
	loggo.Info("finished processing sendCookies",
		"request.sendCookies", len(pq.SendCookies),
		"potentialCookies", len(allPotentialCookies),
		"discarded_cookies", len(allPotentialCookies)-len(pq.SendCookies))
	return ctx, pq
}

// cookieExists searches through the browser's cookie cache for a given server and
// returns true if a cookie with the same Key already exists
func (b *ugglyBrowser) cookieExists(server string, cookie *pb.Cookie) bool {
	if existingCookies, ok := b.cookies[server]; ok {
		for _, ecook := range existingCookies {
			if ecook.Key == cookie.Key {
				return true
			}
		}
	}
	return false
}

// setCookies processes the cookies coming back from a PageResponse and sets them in the browser's
// cookie cache. It overwrites any existing cookies for the same server with the same Key
func (b *ugglyBrowser) setCookies(pr *pb.PageResponse) {
	// since we store cookies under server keys for security
	server := b.sess.server
	novelCount := 0
	for _, rawCookie := range pr.SetCookies {
		// first, let's be nice and set server if none is specified
		// which can make it easier on server devs
		if rawCookie.Server == "" {
			rawCookie.Server = server
		}
		if existingCookies, ok := b.cookies[server]; ok {
			if b.cookieExists(server, rawCookie) {
				for i, ecook := range existingCookies {
					if ecook.Key == rawCookie.Key {
						novelCount++
						loggo.Debug("overwriting existing cookie",
							"rawCookie.Key", rawCookie.Key)
						existingCookies[i] = rawCookie // overwrite existing
					}
				}
			} else {
				novelCount++
				loggo.Debug("adding new cookie", "rawCookie.Key", rawCookie.Key)
				existingCookies = append(existingCookies, rawCookie)
			}
			b.cookies[server] = existingCookies
		} else {
			b.cookies[server] = pr.SetCookies
		}
	}
	loggo.Info("set cookies from server",
		"total-current-server-cookies", len(b.cookies[server]),
		"novel-cookies-added", novelCount,
	)
}
