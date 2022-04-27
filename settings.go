package main

import (
	"strings"
	"strconv"
	"gopkg.in/yaml.v3"
	"io/ioutil"
)

func (b *ugglyBrowser) settingsProcess(formContents map[string]string) {
	loggo.Debug("got settings form submission",
		"formContents", formContents)
	var err error
	changed := false
	for k, v := range formContents {
		loggo.Debug("formData", "k", k, "v", v)
		fv := v
		if k == "VaultFile" {
			if *b.settings.VaultFile != fv {
				loggo.Debug("settings field update",
					"k", k,
					"*b.settings.VaultFile", *b.settings.VaultFile,
					"v", fv)
				b.settings.VaultFile = &fv
				changed = true
			}
		}
		if k == "VaultPassEnvVar" {
			if *b.settings.VaultPassEnvVar != fv {
				loggo.Debug("settings field update",
					"k", k,
					"*b.settings.VaultPassEnvVar", *b.settings.VaultPassEnvVar,
					"v", fv)
				b.settings.VaultPassEnvVar = &fv
				changed = true
			}
		}
		if strings.Contains(k, "bookmark_") {
			// key will come in like "bookmark_ugri_1" where
			// "1" is a string of the bookmark.uid
			// we need this so we know which struct to update
			// ...convering maps to structs is gross, sorry
			chunks := strings.Split(k, "_")
			var stringUidWant string
			if len(chunks) > 2 {
				stringUidWant = chunks[2]
			}
			for _, bm := range(b.settings.Bookmarks) {
				currStringUid := strconv.Itoa(*bm.uid)
				loggo.Debug("bookmark",
					"k", k,
					"v", fv,
					"stringUidWant", stringUidWant,
					"currStringUid", currStringUid)
				if strings.Contains(k, "ugri") && stringUidWant == currStringUid {
					if *bm.Ugri != fv {
						changed = true
						bm.Ugri = &fv
					}
				}
				if strings.Contains(k, "shortname") && stringUidWant == currStringUid  {
					if *bm.ShortName != fv {
						changed = true
						bm.ShortName = &fv
					}
				}
			}
		}
	}
	infoMsg := "no settings were changed"
	if changed {
		infoMsg = "saved settings"
	}
	err = b.settingsSave()
	if err != nil {
		infoMsg = "error saving settings to disk"
	}
	b.sendMessage(infoMsg, "settings-process")
	b.settingsPage(infoMsg)
}

func (b *ugglyBrowser) settingsSave() (err error) {
	filename := b.settingsFile
	bytes, err := yaml.Marshal(b.settings)
	if err != nil {
		loggo.Error("error converting settings to yaml",
			"err", err.Error())
		return err
	}
	loggo.Info("writing settings to disk", "filename", filename)
	err = ioutil.WriteFile(filename, bytes, 0755)
	if err != nil {
		loggo.Error("error writing yaml to file",
			"err", err.Error(),
			"filename", filename)
	}
	return err
}

func (b *ugglyBrowser) settingsLoad() (*ugglyBrowserSettings) {
	filename := b.settingsFile
	s := ugglyBrowserSettings{}
	loggo.Info("reloading settings from file", "filename", filename)
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		loggo.Error("error loading settings yaml file",
			"err", err.Error(),
			"filename", filename)
	} else {
		err = yaml.Unmarshal(data, &s)
	}
	if err != nil {
		loggo.Error("error parsing yaml settings file, loading defaults instead",
			"err", err.Error(),
			"filename", filename)
		defaultVaultPassEnvVar := "UGGSECP"
		defaultVaultFile := "cookies.json.encrypted"
		s = ugglyBrowserSettings{
			VaultPassEnvVar: &defaultVaultPassEnvVar,
			VaultFile:       &defaultVaultFile,
			Bookmarks:       make([]*BookMark, 0),
		}
		err = nil
	}
	s.uidifyBookmarks()
	return &s
}

func (s *ugglyBrowserSettings) uidifyBookmarks() {
	for i, bm := range s.Bookmarks {
		fi := i
		loggo.Debug("assigning bookmark uid",
			"bm.ShortName", bm.ShortName,
			"i", fi)
		bm.uid = &fi
	}
	for _, bm := range s.Bookmarks {
		loggo.Debug("assigned bookmark uid",
			"bm.ShortName", bm.ShortName,
			"bm.uid", *bm.uid)
	}
}

func (s *ugglyBrowserSettings) deleteBookmark(uid int) bool {
	var indexToRemove int
	found := false
	for i, bm := range s.Bookmarks {
		if *bm.uid == uid {
			found = true
			indexToRemove = i
		}
	}
	if found {
		s.Bookmarks = append(s.Bookmarks[:indexToRemove],
			s.Bookmarks[indexToRemove+1:]...)
		s.uidifyBookmarks()
	}
	// found true means we found it and deleted it
	return found
}

func (s *ugglyBrowserSettings) addBookmark(shortName, ugri string) {
	if shortName == "" {
		shortName = "added"
	}
	b := &BookMark{
		ShortName: &shortName,
		Ugri: &ugri,
	}
	s.Bookmarks = append(s.Bookmarks, b)
	s.uidifyBookmarks()
}

type ugglyBrowserSettings struct {
	// the ENV var that stores the vault encryption password
	VaultPassEnvVar *string `yaml:"vaultPassEnvVar"`
	VaultFile       *string `yaml:"vaultFile"`
	Bookmarks       []*BookMark`yaml:"bookMarks"`
}

type BookMark struct {
	Ugri        *string `yaml:"ugri"`
	ShortName   *string `yaml:"shortName"`
	uid         *int
}


