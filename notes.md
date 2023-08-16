# 14/8/23
- The cheesy system is based off of a linksys router. I don't have that so I'm replicating it with two different machines

- A ubiquiti EdgeRouter p6 for routing and firewall/vlan configuration
- And a asus AX53U router as a dumb AP which just passes all calls to the edge router.

- the p6 has no wifi so the network, firewall and main config settings are paced on there. And the wireless settings are configured on the AX53

- Cheesy pulses and accesses the wifi configuration almost every half a second to write a new configuration to the ap, because I've made the AX53U as the ap I've pointed it in code directly to it's ip `10.0.100.3` where the main router p6 is `10.0.100.1`. My theory is that the pulses might cause a bit of lag as it's ssh'inng every half a second to run it's new config. Having the routing and firewall only on the p6 and only the wifi necessary components on the AX should solve any problems, and isolate configuration errors as well to just one device, keeping the driverstations singal intact as it's handled by the switch and main router.

- Curiously the cheesy system sends the wrong signal to the OpenWrt AP, I'm unsure if it's because of a version difference, router difference or just simply a bug.

- Below is an example of what was originally sent from the FMS. In this case it's pulsing the AP with no team as there is either no match running or the match doesn't have a team placed in that slot yet for interface 1. on radio 0
```
uci batch <<ENDCONFIG && wifi radio0
set wireless.@wifi-iface[1].disabled='0'
set wireless.@wifi-iface[1].ssid='no-team-1'
set wireless.@wifi-iface[1].key='no-team-1'
commit wireless
ENDCONFIG
```

- The issue is the `wifi radio0`, which i assume is short hand script for set wifi/radio0 in the up state. But openwrt doesn't detect it as such. So i've changed it to literally specify `wifi up radio0` and now I get correct configurations. (I've yet to test it with a robot)

- My suspicious is that there will need to be vlan tagging of some kind on the dumb AP so when it passes it's info to the main router it's segregated. Might help stability slightly, i'll test both cases. At the moment i'm fairly certain all signals connected to the dumb ap are untagged. Only the driverstations are tagged vlans. They should be able to connect perfectly fine in theory. But it causes a security risk, the driverstations will be unable to access anything important, but malicious signals from the robots might be able to access and cause problems. Depending on if it's tagged with vlan100

# 16/8/23
- Testing with the driverstation on the 3500 series switch I found that the vlan tagging wasn't working, I switched around the config so vlans for driverstations are mapped as such

  - vlan10 -> port 14
  - vlan20 -> port 16
  - vlan30 -> port 18
  - vlan40 -> port 20
  - vlan50 -> port 22
  - vlan60 -> port 24

  - For all other purposes other than port 1 and 3. They're all vlan 100. Port 1 and 3 are also vlan100 but they're trunked to allow extra access between the switch

  - the main problem was the server itself that was hosted on port 3 with an ip of `10.0.100.5`. The subnet requires to be `255.0.0.0` not `255.255.255.0`, a small blunder i forgot about which took me too long to figure out. (to be fair the cheesy arena doesn't exactly mention it either. So... i blame them). But now the server is in range of the variation that the team driverstation ip's can be. I.e `10.97.88.101` etc.. is now able to access the server at `10.0.100.1`

- The radio connection wasn't working previously due to the way that cheesy accesses the ap. it ssh's into the ap and configures the ap in a very VERY specific way
  - The linksys router is very cool, and allows any of the radios to be either 2.4Ghz or 5Ghz. I'm unsure why but the default for the cheesy config linksys is radio0 is 5 and radio1 is 2.4. This is the reverse of normal radio configs where 0 is 2.4, and 1 is 5.
  - The main problem there is our ap is our AX53U, and it has both 2.4Ghz and 5Ghz capabilities. Except like most "dual band" routers/ap's, radio0 is the 2.4, and radio1 is 5. And you can't swap their ranges.
  - This means that for our setup the robots are actually connecting to radio1 not radio0. Not exactly a problem per say. But the cheesy arena only accepts linksys configs with the radio0 configuration.
  - To resolve this I changed the `access_point.go` code around to move to radio1, which also means that when it does it's test/check it's looking on the wrong wlan, the range normally of wlan0, wlan0-1, 0-2, 0-3... etc. Instead it needs wlan1 and wlan1-1 and so on.

  - Some small changes to the code and it was working fine. With the radios able to connect
  ```go
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
  ```

  - However, while they were connected to the FMS and visible, they were in the wrong position. Not vlan position just... visual position. A very strange detection method used by cheesy to determine if the radios are connected or not.
  - Instead of accessing the ap vlan say vlan10 and checking if there is something connected. They grab the full wlan list and count them, and grab the "assumed" configuration of which radio is which.
  - This is terrible, for more reasons than one, but the main issue is the arrangement and order. Because we use radio1 and not radio0, it means that instead of having the arrangement
    - team-1 5Ghz
    - team-2 5Ghz
    - team-3 5Ghz
    - team-4 5Ghz
    - team-5 5Ghz
    - team-6 5Ghz
    - Warp Network 2.4Ghz
  - We instead have the arrangement of
    - Warp Network 2.4Ghz
    - team-1 5Ghz
    - team-2 5Ghz
    - team-3 5Ghz
    - team-4 5Ghz
    - team-5 5Ghz
    - team-6 5Ghz
  - A very small but annoying issue, because the FMS is loaded with data that is exactly 1 position out. And I almost ripped out my hair trying to figure out why the full system assumed this.
  - To resolve this I added some extra code which would detect not only the first 6 like normal, but to filter the 5Ghz interfaces grabbed from the ap and assume "those" were the team ssid radios.
  ```go
  // Parses the given output from the "iwinfo" command on the AP and updates the given status structure with the result.
  func decodeWifiInfo(wifiInfo string, statuses []TeamWifiStatus) error {
    ssidRe := regexp.MustCompile("ESSID: \"([-\\w ]*)\"")
    ssids := ssidRe.FindAllStringSubmatch(wifiInfo, -1)
    linkQualityRe := regexp.MustCompile("Link Quality: ([-\\w ]+)/([-\\w ]+)")
    linkQualities := linkQualityRe.FindAllStringSubmatch(wifiInfo, -1)
    GHzRe := regexp.MustCompile(`Channel: \d+ \((\d+\.\d{1,3}) GHz\)`)
    GHzFrequencies := GHzRe.FindAllStringSubmatch(wifiInfo, -1)

    teamInterfaces := 0

    for i := range ssids {
      if !strings.Contains(GHzFrequencies[i][1], "2.4") {
        ssid := ssids[i][1]
        linkQualityNumerator := linkQualities[i][1]
        statuses[teamInterfaces].TeamId, _ = strconv.Atoi(ssid) // Convert non-numeric SSIDs to zero
        statuses[teamInterfaces].RadioLinked = linkQualityNumerator != "unknown"
        teamInterfaces++
      } else {
        // log.Printf("Skipping interface %d: %v", i, ssids[i])
      }
    }

    return nil
  }
  ```

  - I did also think about completely changing this design and using a proper vlan tester instead. But... i thought it best to keep it as much as it was as possible. And change only what's necessary.

- The vlan system between the ap, router and switch has now been finalised
  - The ap has all the wifi radios connected to 7 interfaces, 6 for the teams, 1 for generic connections
  - The ap tags them all with either vlan10,20,30 etc... and 100 for the generic connections
  - The ap is configured as a dumb ap pointing everything to the router, and it has no firewall. It speeds up the reboot cycles and configuration speeds quite a bit. Not very fast, but every little bit counts.
  - The router takes the tagged and untagged signals from lan1 and moves them to lan0. Which connects directly to port 3 on the switch.
  - While the router also handles dhcp and actual routing for most normal clients it surprisingly doesn't actually do much to the driverstations and radios. The ap is just an ap and it's a bridge to the robot radios so dhcp isn't a problem as the radio itself has it's own ip and sets the ip of either the roborio or any other clients connected.
  - And for the driverstations the switch is configured every new match to provide a new ip to that port. So anything that connects to it auto reconfigures between the range the switch sets. Effectively manually setting it to a static ip, for instance `10.97.88.101`.
  - The process of connecting between the switch and the ap is so segregated through vlans that the router can be taken completely out of the equation and the system would actually still work perfectly fine without it.
  - The exception being the firewall, other than needing routing and dhcp for normal devices like computers that aren't the server. The router effectively only has one job, and that's to check the data between it's lan0 and lan1 for breaches in the FRC accept and reject list of allowed ip, configurations and ports. This can be handled on the ap as well, removing the router all together, but as mentioned before. This does speed up the system slightly as all routing and firewall checking of every single packet is done on the faster ubiquity router instead of the ap. Leaving the ap to keep a steady wifi connection. It also adds extra ability for other computers to connect via normal dhcp and such, but in the event that no other computers other than the server are connected to the system. The router only handles the firewall between the driverstations and the radios.