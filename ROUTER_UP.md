## CONSTRUCTION DU ROUTER
*Condition: L'ordinateur doit avoir deux adapteurs réseaux prêts à utiliser. Le premier pour la connexion internet (wan) et le second pour le réseau local (lan). Pour la suite, on considère que l'adpateur ethernet est connecté au WAN et l'adapteur wifi servira de gateway pour le lan*

### Dépendances requise
- dnsmasq
- hostap

## Installation
### Pour Debian/Ubuntu
    sudo apt update  
    sudo apt install dnsmasq  
    sudo apt install hostapd

## Configuration hostapd: 
Le nom de l'interface wifi dans mon cas est *wlo1* mais il est à vous de savoir le votre en faisant *ip a*
- Créer le fichier **/etc/NetworManager/10-ignore-wlo1.conf**
- Y ajouter:  
  
    [keyfile]  
    unmanaged-devices=interface-name:wlo1
- Réinitialiser NetworkManager: 
**sudo systemctl restart NetworkManager**
- Assigner statiquement un ip à l'interface:
 **sudo ip a add 10.0.0.1/24 dev wlo1**
- Créer le fichier : */etc/hostapd/hostapd.conf* et remplir l' **interface**, le **ssid** ansi que le **mot de passe** du point d'accès router selon votre appareil

    interface=wlo1 
    driver=nl80211  
    ssid=MyDebianHotspot
    channel=11  
    hw_mode=g  
    wpa=2  
    wpa_passphrase=MySecureHotspotPassword 
    wpa_key_mgmt=WPA-PSK  
    rsn_pairwise=CCMP  
    ignore_broadcast_ssid=0

- Réinitialiser hostapd: **sudo systemctl restart hostapd**

## Configuration dnsmasq
Dnsmasq est le paquet de service DHCP pour le LAN
- modifier ou créer le fichier **/etc/dnsmasq.conf**
- Y mettre: 

    interface=wlo1  
    dhcp-range=10.0.0.10,10.0.0.100,12h  
    dhcp-option=3,10.0.0.1  
    dhcp-option=6,8.8.8.8,8.8.4.4  
    bind-interfaces*

- Réinitialiser dnsmaq: **sudo systemctl restart dnsmasq**


## Vérification:
Vérification que chaque service fonctionne sans erreur:

    systemctl status hostapd
    systemctl status dnsmasq