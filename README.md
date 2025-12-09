## QoS Traffic Control Backend

Ce projet est une application backend Go (qos-app) qui utilise le sous-système Linux Traffic Control (tc) pour configurer des règles de qualité de service (QoS), de limitation de bande passante (shaping) et de latence sur des interfaces réseau spécifiées.

ATTENTION : Cette application nécessite des droits root (sudo) pour exécuter les commandes tc sur le système d'exploitation.

### 1. Dépendances requises

Pour compiler et exécuter ce projet, vous aurez besoin des éléments suivants :



- Go (Langage):Version 1.20 ou supérieure.

- Linux Traffic Control (tc), fait partie du package iproute2 sur la plupart des distributions Linux (installé par défaut).

- swag: Outil de génération de documentation Swagger.

#### Méthode d'installation

##### Installation de Go

Téléchargez la dernière version stable de Go depuis le site officiel de Go.

Sur la plupart des systèmes basés sur Debian/Ubuntu ou RHEL/CentOS :

###### Pour Debian/Ubuntu
    sudo apt update
    sudo apt install golang

##### OU pour RHEL/CentOS
    sudo yum install golang

#### Vérification de l'installation
    go version


#### Installation de swag (Pour la documentation)
L'outil swag est nécessaire pour générer la documentation Swagger à partir des annotations du code :

**go install [github.com/swaggo/swag/cmd/swag@latest](https://github.com/swaggo/swag/cmd/swag@latest)**

### IMPORTANT : Assurez-vous que $HOME/go/bin est dans votre variable PATH. 
Si vous obtenez "swag: command not found", vous devez l'ajouter. Faites ceci dans le cas contraire:
#### Ajout des exécutable dans PATH:
- Modifier *.bashrc* en effectuant:  **nano ~/.bashrc**
- Y ajouter à la fin **export PATH=$PATH:~/go/bin**
- Exécuter **source ~/.bashrc** pour la rendre effective
 
#### Installation des packages Go du projet

Une fois Go installé, naviguez jusqu'à la racine du projet et installez les dépendances :

    go mod tidy


### 2. Compilation de l'Application

Vous pouvez compiler l'application pour créer l'exécutable qos-app :

    go build -o qos-app ./cmd/api


Ceci créera le fichier exécutable ./qos-app à la racine de votre projet.

### 3. Génération de la Documentation Swagger

Avant l'exécution, vous devez générer la documentation API :

Naviguez vers le répertoire de votre point d'entrée :

    cd cmd/api


Générez la documentation (place le dossier docs à la racine du projet) :

    swag init -g main.go -dir .,../../internal/adapter/handler -o ../../docs


Retournez à la racine du projet :

    cd ../..

Le dossier ./docs est maintenant prêt et contient le swagger.json.

### 4. Utilisation et Exécution

L'application doit être exécutée avec des droits root et nécessite deux arguments : l'interface LAN et l'interface WAN.

Syntaxe d'Exécution

    sudo ./qos-app \<interface-lan> \<interface-wan>


Exemple : Si votre interface LAN est wlo1 et votre interface WAN est eno2 :

    sudo ./qos-app wlo1 eno2

1. Endpoints de l'API

Le serveur écoute sur http://localhost:8080.

Accès à la Documentation Interactive

L'interface Swagger est disponible à l'adresse suivante :

http://localhost:8080/swagger/index.html


Vous pouvez utiliser cette interface pour tester tous les endpoints ci-dessous.
| Méthode | Chemin | Description | Tags | 
| ----- | ----- | ----- | ----- | 
| `POST` | `/qos/setup` | **Initialise** la structure HTB (Hierarchical Token Bucket) sur les deux interfaces avec une bande passante globale maximale. | HTB Setup | 
| `POST` | `/qos/htb/limit` | **Met à jour** la limite globale de bande passante (`rate/ceil`) pour la structure HTB existante. | HTB Setup | 
| `POST` | `/qos/simple/limit` | **Applique** une limitation simple (TBF - Token Bucket Filter) sur les deux interfaces, effaçant toute configuration HTB. | Simple TBF | 
| `POST` | `/qos/reset` | **Supprime** toutes les disciplines de file d'attente (QDiscs) des deux interfaces, réinitialisant la mise en forme. | Cleanup |


Exemple d'Utilisation avec curl

### 1. Initialiser la structure HTB à 100Mbit/s :

    curl -X POST http://localhost:8080/qos/setup -H "Content-Type: application/json" -d '{"total_bandwidth": "100mbit"}'

### 2. Appliquer une limite HTB de 50Mbit/s :

    curl -X PUT http://localhost:8080/qos/simple/limit -H "Content-Type: application/json" -d '{"rate_limit": "50mbit"}'


### 2. Appliquer une limite simple TBF de 50Mbit/s :

    curl -X POST http://localhost:8080/qos/htb/global/limit -H "Content-Type: application/json" -d '{"rate_limit": "50mbit", "latency": "50ms"}'

### 3. Réinitialiser la mise en forme (Cleanup) :

    curl -X POST http://localhost:8080/qos/reset -H "Content-Type: application/json" -d '{"lan_interface": "wlo1", "wan_interface": "eno2"}'
