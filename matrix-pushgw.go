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
	"strings"
	"syscall"
	"sync"
	"bytes"
	
	"github.com/ubports/ubuntu-push/logger"
)

type Content struct {
	Body           string
	Format         string
	Formatted_body string
	Msgtype        string
}

type Counts struct {
	Unread int
}

type DeviceData struct {
}

type Tweaks struct {
	Highlight  bool
	Sound      string
}

type Device struct {
	App_id     string
	Data       DeviceData
	Pushkey    string
	Pushkey_ts int
	Tweaks     Tweaks
}

type Notification struct {
	Content             Content
	Counts              Counts
	Devices             []Device
	Event_id            string
	Id                  string
	Room_id             string
	Sender              string
	Sender_display_name string
	Type                string
}

type PushNotification struct {
	Notification Notification
}

type DevMsg struct {
	MsgType int
	Error   bool
}

type UbuntuTouchNotification struct {
	AppId string `json:"appid"`
	ExpireOn string `json:"expire_on"`
	Token string `json:"token"`
	ClearPending bool `json:"clear_pending"`
	ReplaceTag string `json:"replace_tag"`
	Data SmallNotification `json:"data"`
}

type SmallNotification struct {
	Content             Content
	Counts              Counts
	Event_id            string
	Id                  string
	Room_id             string
	Sender              string
	Sender_display_name string
	Type                string

}

func handlePush(w http.ResponseWriter, r *http.Request) {
	_logger.Infof("handlePush() was called, trying to parse & dump plain notification JSON:")

	bodybytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		_logger.Errorf(err.Error())
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	body := string(bodybytes)

	fmt.Println(body)

	dec := json.NewDecoder(strings.NewReader(body))
	var n PushNotification
	err = dec.Decode(&n)
	if err != nil {
		_logger.Errorf("Error parsing JSON: %s", err.Error())
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	_logger.Infof("Iterating through device list")

	for _, d := range n.Notification.Devices {
		_logger.Infof("Processing notification for push key %s", d.Pushkey)
		//TODO: Send modified JSON request to localhost:5001 in UT push format
		smallNote := SmallNotification {
			Content: n.Notification.Content,
			Counts: n.Notification.Counts,
			Event_id: n.Notification.Event_id,
			Id: n.Notification.Id,
			Room_id: n.Notification.Room_id,
			Sender: n.Notification.Sender,
			Sender_display_name: n.Notification.Sender_display_name,
			Type: n.Notification.Type}
			
		m := UbuntuTouchNotification {
			AppId: d.App_id,
			ExpireOn: "2019-10-08T14:48:00.000Z",
			Token: d.Pushkey,
			ClearPending: false,
			ReplaceTag: n.Notification.Event_id,
			Data: smallNote}
		b, _ := json.Marshal(m)
		fmt.Println(string(b))
		client := &http.Client{}
		r, _ := http.NewRequest("POST", "https://push.ubports.com/notify", bytes.NewBuffer(b))
		r.Header.Add("Content-Type", "application/json")
		resp, err := client.Do(r)
		if err != nil {
			_logger.Errorf("Error relaying push JSON to Ubuntu Touch push server: %s", err.Error())
		}
		defer r.Body.Close()
		_logger.Infof("response from Ubuntu Touch push server: %s", resp.Status)
	}

	fmt.Println("done")

	w.Write([]byte("{}"))
}

type Config struct {
	PlainPort      int
	SslPort        int
	Debug          bool
	DebugWS        bool
	PushServerUrl  string
	PushServerPort int
}

func listenPlainHTTP(wg *sync.WaitGroup) {
	defer wg.Done()

	if gConfig.PlainPort == 0 {
		fmt.Println("Plain HTTP port not configured")
		return
	}

	err := http.ListenAndServe(":"+strconv.Itoa(gConfig.PlainPort), nil)

	if err != nil {
		fmt.Println("Can't listen on plain HTTP port:", err.Error())
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

func main() {
	file, _ := os.Open("matrix-pushgw.conf")
	defer file.Close()
	decoder := json.NewDecoder(file)
	config := new(Config)

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

		var wg sync.WaitGroup
		wg.Add(2)

		go listenPlainHTTP(&wg)

		wg.Wait()
	}

	os.Exit(0)
}
