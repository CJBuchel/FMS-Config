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