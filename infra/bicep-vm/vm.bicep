@description('Location.')
param location string

param vmName string
param vmSize string
param adminUsername string

@description('SSH public key contents.')
param sshPublicKey string

@description('Resource ID of the NIC to attach.')
param nicId string

// Installs Docker Engine + the Compose plugin only. No app secrets, no
// app files — infra/scripts/deploy-vm.sh pushes those afterwards over
// `az vm run-command` (which runs as root, so there's no need to add
// adminUsername to the docker group here for that flow — do it manually
// with `sudo usermod -aG docker $USER` if you also want passwordless
// `docker` over SSH).
//
// Note: Bicep's triple-quoted strings are NOT interpolated (no ${...}
// substitution) — that's deliberate here, since this whole block is
// meant to be taken literally and passed through to cloud-init untouched,
// shell variables (`$(...)`, `$VERSION_CODENAME`) included.
var cloudInit = base64('''#cloud-config
package_update: true
package_upgrade: true
packages:
  - ca-certificates
  - curl
runcmd:
  - install -m 0755 -d /etc/apt/keyrings
  - curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  - chmod a+r /etc/apt/keyrings/docker.asc
  - echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo \"$VERSION_CODENAME\") stable" > /etc/apt/sources.list.d/docker.list
  - apt-get update
  - apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
  - mkdir -p /opt/copilot-web
''')

resource vm 'Microsoft.Compute/virtualMachines@2024-07-01' = {
  name: vmName
  location: location
  properties: {
    hardwareProfile: {
      vmSize: vmSize
    }
    osProfile: {
      computerName: vmName
      adminUsername: adminUsername
      customData: cloudInit
      linuxConfiguration: {
        disablePasswordAuthentication: true
        ssh: {
          publicKeys: [
            {
              path: '/home/${adminUsername}/.ssh/authorized_keys'
              keyData: sshPublicKey
            }
          ]
        }
      }
    }
    storageProfile: {
      imageReference: {
        publisher: 'Canonical'
        offer: 'ubuntu-24_04-lts'
        sku: 'server'
        version: 'latest'
      }
      osDisk: {
        createOption: 'FromImage'
        managedDisk: {
          storageAccountType: 'StandardSSD_LRS'
        }
        diskSizeGB: 30
      }
    }
    networkProfile: {
      networkInterfaces: [
        {
          id: nicId
        }
      ]
    }
  }
}

output vmName string = vm.name
