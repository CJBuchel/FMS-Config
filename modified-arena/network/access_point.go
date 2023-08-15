// Copyright 2017 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Methods for configuring a Linksys WRT1900ACS access point running OpenWRT for team SSIDs and VLANs.

package network

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Team254/cheesy-arena/model"
	"golang.org/x/crypto/ssh"
)

const (
	accessPointSshPort                = 22
	accessPointConnectTimeoutSec      = 1
	accessPointCommandTimeoutSec      = 5
	accessPointPollPeriodSec          = 3
	accessPointRequestBufferSize      = 10
	accessPointConfigRetryIntervalSec = 5
)

type AccessPoint struct {
	address                string
	username               string
	password               string
	teamChannel            int
	networkSecurityEnabled bool
	configRequestChan      chan [6]*model.Team
	TeamWifiStatuses       [6]TeamWifiStatus
	initialStatusesFetched bool
}

type TeamWifiStatus struct {
	TeamId      int
	RadioLinked bool
	MBits       float64
}

type sshOutput struct {
	output string
	err    error
}

func (ap *AccessPoint) SetSettings(address, username, password string, teamChannel int, networkSecurityEnabled bool) {
	ap.address = address
	ap.username = username
	ap.password = password
	ap.teamChannel = teamChannel
	ap.networkSecurityEnabled = networkSecurityEnabled

	// Create config channel the first time this method is called.
	if ap.configRequestChan == nil {
		ap.configRequestChan = make(chan [6]*model.Team, accessPointRequestBufferSize)
	}
}

// Loops indefinitely to read status from and write configurations to the access point.
func (ap *AccessPoint) Run() {
	var bypassConfig bool = true
	if bypassConfig {
		log.Printf("CJ Hack set, bypass config and hardcode addresses and auth")
	}

	for {
		// Check if there are any pending configuration requests; if not, periodically poll wifi status.
		select {
		case request := <-ap.configRequestChan:
			// If there are multiple requests queued up, only consider the latest one.
			numExtraRequests := len(ap.configRequestChan)
			for i := 0; i < numExtraRequests; i++ {
				request = <-ap.configRequestChan
			}

			ap.handleTeamWifiConfiguration(request, bypassConfig)
		case <-time.After(time.Second * accessPointPollPeriodSec):
			ap.updateTeamWifiStatuses(bypassConfig)
			ap.updateTeamWifiBTU(bypassConfig)
		}
	}
}

// Adds a request to set up wireless networks for the given set of teams to the asynchronous queue.
func (ap *AccessPoint) ConfigureTeamWifi(teams [6]*model.Team) error {
	// Use a channel to serialize configuration requests; the monitoring goroutine will service them.
	select {
	case ap.configRequestChan <- teams:
		return nil
	default:
		return fmt.Errorf("WiFi config request buffer full")
	}
}

func (ap *AccessPoint) handleTeamWifiConfiguration(teams [6]*model.Team, bypassConfig bool) {
	if !ap.networkSecurityEnabled {
		return
	}

	if ap.configIsCorrectForTeams(teams) {
		return
	}

	// Clear the state of the radio before loading teams.
	ap.configureTeams([6]*model.Team{nil, nil, nil, nil, nil, nil}, bypassConfig)
	ap.configureTeams(teams, bypassConfig)
}

func (ap *AccessPoint) configureTeams(teams [6]*model.Team, bypassConfig bool) {
	retryCount := 1

	for {
		teamIndex := 0
		for teamIndex < 6 {
			config, err := generateTeamAccessPointConfig(teams[teamIndex], teamIndex+1)
			if err != nil {
				log.Printf("Failed to generate WiFi configuration: %v", err)
			}

			command := addConfigurationHeader(config)

			_, err = ap.runCommand(command, bypassConfig)
			if err != nil {
				log.Printf("Error writing team configuration to AP %v", err)
				retryCount++
				time.Sleep(time.Second * accessPointConfigRetryIntervalSec)
				continue
			}

			teamIndex++
		}
		time.Sleep(time.Second * accessPointConfigRetryIntervalSec)
		err := ap.updateTeamWifiStatuses(bypassConfig)
		if err == nil && ap.configIsCorrectForTeams(teams) {
			log.Printf("Successfully configured WiFi after %d attempts.", retryCount)
			break
		}
		log.Printf("WiFi configuration still incorrect after %d attempts; trying again.", retryCount)
	}
}

// Returns true if the configured networks as read from the access point match the given teams.
func (ap *AccessPoint) configIsCorrectForTeams(teams [6]*model.Team) bool {
	if !ap.initialStatusesFetched {
		return false
	}

	for i, team := range teams {
		expectedTeamId := 0
		if team != nil {
			expectedTeamId = team.Id
		}
		if ap.TeamWifiStatuses[i].TeamId != expectedTeamId {
			return false
		}
	}

	return true
}

// Fetches the current wifi network status from the access point and updates the status structure.
func (ap *AccessPoint) updateTeamWifiStatuses(bypassConfig bool) error {
	if !ap.networkSecurityEnabled {
		return nil
	}

	output, err := ap.runCommand("iwinfo", bypassConfig)
	if err == nil {
		err = decodeWifiInfo(output, ap.TeamWifiStatuses[:])
	}

	if err != nil {
		return fmt.Errorf("Error getting wifi info from AP: %v", err)
	} else {
		if !ap.initialStatusesFetched {
			ap.initialStatusesFetched = true
		}
	}
	return nil
}

// Logs into the access point via SSH and runs the given shell command.
func (ap *AccessPoint) runCommand(command string, bypassConfig bool) (string, error) {
	// Open an SSH connection to the AP.
	// @TODO switch back to using coded username and password
	var config *ssh.ClientConfig
	var sshPort int
	var address string
	if bypassConfig {
		config = &ssh.ClientConfig{User: "root",
			Auth:            []ssh.AuthMethod{ssh.Password("ubnt")},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         accessPointConnectTimeoutSec * time.Second}
		address = "10.0.100.3"
		sshPort = 22
	} else {
		config = &ssh.ClientConfig{User: ap.username,
			Auth:            []ssh.AuthMethod{ssh.Password(ap.password)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         accessPointConnectTimeoutSec * time.Second}
		address = ap.address
		sshPort = accessPointSshPort
	}

	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", address, sshPort), config)

	if err != nil {
		log.Printf("Error connecting to AP using tcp dial up: %v", err)
		log.Printf("Address was: %s, Username was: %s, Password was: %s", ap.address, ap.username, ap.password)
		return "", err
	}
	session, err := conn.NewSession()
	if err != nil {
		log.Printf("Error creating session with AP")
		return "", err
	}
	defer session.Close()
	defer conn.Close()

	// Run the command with a timeout.
	commandChan := make(chan sshOutput, 1)
	go func() {
		outputBytes, err := session.Output(command)
		commandChan <- sshOutput{string(outputBytes), err}
	}()
	select {
	case output := <-commandChan:
		if output.err != nil {
			log.Printf("Error running command: %s, on AP", command)
		}
		return output.output, output.err
	case <-time.After(accessPointCommandTimeoutSec * time.Second):
		return "", fmt.Errorf("WiFi SSH command timed out after %d seconds", accessPointCommandTimeoutSec)
	}
}

func addConfigurationHeader(commandList string) string {
	return fmt.Sprintf("uci batch <<ENDCONFIG && wifi up radio1\n%s\ncommit wireless\nENDCONFIG\n", commandList)
}

// Verifies WPA key validity and produces the configuration command for the given team.
func generateTeamAccessPointConfig(team *model.Team, position int) (string, error) {
	if position < 1 || position > 6 {
		return "", fmt.Errorf("invalid team position %d", position)
	}

	commands := &[]string{}
	if team == nil {
		*commands = append(*commands, fmt.Sprintf("set wireless.@wifi-iface[%d].disabled='0'", position),
			fmt.Sprintf("set wireless.@wifi-iface[%d].ssid='no-team-%d'", position, position),
			fmt.Sprintf("set wireless.@wifi-iface[%d].key='no-team-%d'", position, position))
	} else {
		if len(team.WpaKey) < 8 || len(team.WpaKey) > 63 {
			return "", fmt.Errorf("invalid WPA key '%s' configured for team %d", team.WpaKey, team.Id)
		}

		*commands = append(*commands, fmt.Sprintf("set wireless.@wifi-iface[%d].disabled='0'", position),
			fmt.Sprintf("set wireless.@wifi-iface[%d].ssid='%d'", position, team.Id),
			fmt.Sprintf("set wireless.@wifi-iface[%d].key='%s'", position, team.WpaKey))
	}

	return strings.Join(*commands, "\n"), nil
}

// Parses the given output from the "iwinfo" command on the AP and updates the given status structure with the result.
func decodeWifiInfo(wifiInfo string, statuses []TeamWifiStatus) error {
	ssidRe := regexp.MustCompile("ESSID: \"([-\\w ]*)\"")
	ssids := ssidRe.FindAllStringSubmatch(wifiInfo, -1)
	linkQualityRe := regexp.MustCompile("Link Quality: ([-\\w ]+)/([-\\w ]+)")
	linkQualities := linkQualityRe.FindAllStringSubmatch(wifiInfo, -1)

	// There should be six networks present -- one for each team on the 5GHz radio.
	if len(ssids) < 6 || len(linkQualities) < 6 {
		return fmt.Errorf("Could not parse wifi info; expected 6 team networks, got %d.", len(ssids))
	}

	for i := range statuses {
		ssid := ssids[i][1]
		statuses[i].TeamId, _ = strconv.Atoi(ssid) // Any non-numeric SSIDs will be represented by a zero.
		linkQualityNumerator := linkQualities[i][1]
		statuses[i].RadioLinked = linkQualityNumerator != "unknown"
	}

	return nil
}

// Polls the 6 wlans on the ap for bandwith use and updates data structure.
func (ap *AccessPoint) updateTeamWifiBTU(bypassConfig bool) error {
	if !ap.networkSecurityEnabled {
		return nil
	}

	infWifi := []string{"1", "1-1", "1-2", "1-3", "1-4", "1-5"}
	for i := range ap.TeamWifiStatuses {

		output, err := ap.runCommand(fmt.Sprintf("luci-bwc -i wlan%s", infWifi[i]), bypassConfig)
		if err == nil {
			btu := parseBtu(output)
			ap.TeamWifiStatuses[i].MBits = btu
		}
		if err != nil {
			return fmt.Errorf("Error getting BTU info from AP: %v", err)
		}
	}
	return nil
}

// Parses Bytes from ap's onboard bandwith monitor returns 5 sec average bandwidth in Megabits per second for the given data.
func parseBtu(response string) float64 {
	mBits := 0.0
	lines := strings.Split(response, "],")
	if len(lines) > 6 {
		fiveCnt := strings.Split(strings.TrimRight(strings.TrimLeft(strings.TrimSpace(lines[len(lines)-6]), "["), "]"), ",")
		lastCnt := strings.Split(strings.TrimRight(strings.TrimLeft(strings.TrimSpace(lines[len(lines)-1]), "["), "]"), ",")
		rXBytes, _ := strconv.Atoi(strings.TrimSpace(lastCnt[1]))
		tXBytes, _ := strconv.Atoi(strings.TrimSpace(lastCnt[3]))
		rXBytesOld, _ := strconv.Atoi(strings.TrimSpace(fiveCnt[1]))
		tXBytesOld, _ := strconv.Atoi(strings.TrimSpace(fiveCnt[3]))
		mBits = float64(rXBytes-rXBytesOld+tXBytes-tXBytesOld) * 0.000008 / 5.0
	}
	return mBits
}
