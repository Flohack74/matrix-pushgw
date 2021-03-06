/* matrix-push.go - A Matrix Push Gateway */

/*
 *  Copyright (c) 2016 Sergio L. Pascual <slp@sinrega.org>
 *  All rights reserved.
 *
 *  This program is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  This program is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *  GNU General Public License for more details.
 *
 *  You should have received a copy of the GNU General Public License
 *  along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"sync"
	"bytes"
	"time"
	"io"
	"runtime"

	"github.com/ubports/ubuntu-push/logger"
)

type Content struct {
	Body           string `json:"body"`
	Format         string `json:"format"`
	Formatted_body string `json:"formatted_body"`
	Msgtype        string `json:"msgtype"`
	Membership     string `json:"membership"`
}

type Counts struct {
	Unread 	     int `json:"unread"`
	Missed_calls int `json:"missed_calls"`
}

type PusherData struct {
	Format string `json:"format"`
}

type Tweaks struct {
	Highlight  bool `json:"highlight"`
	Sound      string `json:"sound"`
}

type Device struct {
	App_id     string `json:"app_id"`
	Data       PusherData `json:"data"`
	Pushkey    string `json:"pushkey"`
	Pushkey_ts int `json:"pushkey_ts"`
	Tweaks     Tweaks `json:"tweaks"`
}

type Notification struct {
	Content             Content `json:"content"`
	Counts              Counts `json:"counts"`
	Devices             []Device `json:"devices"`
	Event_id            string `json:"event_id"`
	Id                  string `json:"id"`
	Room_id             string `json:"room_id"`
	Room_Name           *string `json:"room_name,omitempty"`
	Room_Alias          string `json:"room_alias"`
	User_Is_Target	    bool `json:"user_is_target"`
	Prio                string `json:"prio"`
	Sender              string `json:"sender"`
	Sender_display_name string `json:"sender_display_name"`
	Type                string `json:"type"`
}

type Message struct {
	Message Notification `json:"message"`
}

type PushNotification struct {
	Notification Notification
}

type DevMsg struct {
	MsgType int `json:"msgtype"`
	Error   bool `json:"error"`
}

type UbuntuTouchNotification struct {
	AppId string `json:"appid"`
	ExpireOn string `json:"expire_on"`
	Token string `json:"token"`
	ClearPending bool `json:"clear_pending"`
	ReplaceTag string `json:"replace_tag"`
	Data Message `json:"data"`
}

const expiryWeeks = 10

func handlePush(w http.ResponseWriter, r *http.Request) {
	_logger.Infof("handlePush() was called, trying to parse & dump plain notification JSON:")
	dec := json.NewDecoder(r.Body)
	var n PushNotification
	err := dec.Decode(&n)
	if err != nil {
		_logger.Errorf("Error parsing JSON: %s", err.Error())
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	var message Message
	message.Message = n.Notification
	_logger.Infof("Iterating through device list")
	for _, d := range n.Notification.Devices {
		_logger.Infof("Processing notification for push key %s", d.Pushkey)
		expire := time.Now().AddDate(0, 0, 7 * expiryWeeks)
		m := UbuntuTouchNotification {
			AppId: d.App_id,
			ExpireOn: expire.Format(time.RFC3339),
			Token: d.Pushkey,
			ClearPending: n.Notification.Counts.Unread == 0,
			ReplaceTag: n.Notification.Room_id,
			Data: message}
		b, _ := json.Marshal(m)
		apiRequest, _ := http.NewRequest("POST", gConfig.UbuntuTouchPushServerUrl, bytes.NewBuffer(b))
		apiRequest.Header.Add("Content-Type", "application/json")
		resp, err := localClient.Do(apiRequest)
		if err != nil {
			_logger.Errorf("Error relaying push JSON to Ubuntu Touch push server: %s", err.Error())
			fmt.Println(string(b))
		}
		defer resp.Body.Close()
		io.Copy(ioutil.Discard, resp.Body)
		_logger.Infof("response from Ubuntu Touch push server: %s", resp.Status)
	}
	n.Notification.Room_Name = nil
	_logger.Infof("handlePush() done")

	w.Write([]byte("{}"))
}

type Config struct {
	PlainPort      int
	SslPort        int
	Debug          bool
	DebugWS        bool
	PushServerUrl  string
	PushServerPort int
	ServerCrtFile  string
	ServerKeyFile  string
	UbuntuTouchPushServerUrl string
}

func listenHTTP(wg *sync.WaitGroup) {
	defer wg.Done()

	if (gConfig.PlainPort == 0) {
		_logger.Errorf("HTTP not configured, returning")
		return
	}

	if (gConfig.UbuntuTouchPushServerUrl == "") {
		gConfig.UbuntuTouchPushServerUrl = "https://push.ubports.com/notify"
	}
	serverCrtFile := gConfig.ServerCrtFile
	if (serverCrtFile == "") {
	    serverCrtFile = "server.crt"
	}
	serverKeyFile := gConfig.ServerKeyFile
	if (serverKeyFile == "") {
	    serverKeyFile = "server.key"
	}
	fmt.Printf("Using the following configuration variables: %+v\n", gConfig)
	if gConfig.SslPort != 0 {
	    err := http.ListenAndServeTLS(":"+strconv.Itoa(gConfig.SslPort), serverCrtFile, serverKeyFile, nil)
        if err != nil {
            _logger.Errorf("Can't listen on HTTPS port: %s", err)
		}
	}

    if gConfig.SslPort != 0 {
        err := http.ListenAndServe(":"+strconv.Itoa(gConfig.PlainPort), nil)
        if err != nil {
		    _logger.Errorf("Can't listen on HTTP port: %s", err.Error())
	    }
    }
}

func signalHandler(c *chan os.Signal) {
	for s := range *c {
		if s == syscall.SIGHUP {
			_logger.Infof("Received " + s.String() + " signal, reloading")
		} else {
			_logger.Infof("Received " + s.String() + " signal, bailing out")
			os.Exit(0)
		}
	}
}

var gConfig *Config
var _logger logger.Logger
var localClient *http.Client

func main() {
	file, _ := os.Open("matrix-pushgw.conf")
	defer file.Close()
	decoder := json.NewDecoder(file)
	config := new(Config)
	localClient = &http.Client{Timeout: time.Second * 10,}
	err := decoder.Decode(config)
	if err != nil {
		fmt.Println("Can't open configuration file:", err)
	} else {
		gConfig = config
		signal_channel := make(chan os.Signal, 1)
		signal.Notify(signal_channel, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		go signalHandler(&signal_channel)

		_logger = logger.NewSimpleLogger(os.Stderr, "info")
		_logger.Infof("Starting Matrix Push Gateway...")
		http.HandleFunc("/_matrix/push/r0/notify", handlePush)
		//See https://github.com/matrix-org/synapse/pull/8865
		http.HandleFunc("/_matrix/push/v1/notify", handlePush)
		var wg sync.WaitGroup
		wg.Add(1)
		go listenHTTP(&wg)

		wg.Wait()
	}

	os.Exit(0)
}
