# root-chart

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 1.16.0](https://img.shields.io/badge/AppVersion-1.16.0-informational?style=flat-square)

Root Chart to a single Service

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| file://../consumer_chart | cdc(golang-chart) | 0.1.0 |
| file://../consumer_chart | poller(golang-chart) | 0.1.0 |
| oci://ghcr.io/atomicloud/nitroso.helium | helium(root-chart) | 1.3.0 |
| oci://ghcr.io/atomicloud/nitroso.zinc | zinc(root-chart) | 1.6.5 |
| oci://ghcr.io/atomicloud/sulfoxide.bromine | bromine(sulfoxide-bromine) | 1.3.0 |
| oci://ghcr.io/dragonflydb/dragonfly/helm | livecache(dragonfly) | v1.13.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| bromine.annotations."argocd.argoproj.io/sync-wave" | string | `"1"` |  |
| bromine.enable | bool | `false` |  |
| bromine.rootSecret | object | `{"ref":"NITROSO_TIN"}` | Secret of Secrets reference |
| bromine.rootSecret.ref | string | `"NITROSO_TIN"` | DOPPLER Token Reference |
| bromine.storeName | string | `"nitroso-tin"` | Store name to create |
| bromine.target | string | `"nitroso-tin"` |  |
| cdc.affinity | object | `{}` |  |
| cdc.annotations."argocd.argoproj.io/sync-wave" | string | `"4"` |  |
| cdc.appSettings | object | `{}` |  |
| cdc.autoscaling | object | `{}` |  |
| cdc.configMountPath | string | `"/app/config"` |  |
| cdc.enabled | bool | `true` |  |
| cdc.envFromSecret | string | `"nitroso-tin"` |  |
| cdc.image.pullPolicy | string | `"IfNotPresent"` |  |
| cdc.image.repository | string | `"nitroso-tin-cdc"` |  |
| cdc.image.tag | string | `""` |  |
| cdc.imagePullSecrets | list | `[]` |  |
| cdc.jobRbac.create | bool | `false` |  |
| cdc.nameOverride | string | `"tin-cdc"` |  |
| cdc.nodeSelector | object | `{}` |  |
| cdc.podAnnotations | object | `{}` |  |
| cdc.podSecurityContext | object | `{}` |  |
| cdc.replicaCount | int | `1` |  |
| cdc.resources | object | `{}` |  |
| cdc.securityContext | object | `{}` |  |
| cdc.serviceAccount.annotations | object | `{}` |  |
| cdc.serviceAccount.create | bool | `false` |  |
| cdc.serviceAccount.name | string | `""` |  |
| cdc.serviceTree.<<.landscape | string | `"lapras"` |  |
| cdc.serviceTree.<<.layer | string | `"2"` |  |
| cdc.serviceTree.<<.platform | string | `"nitroso"` |  |
| cdc.serviceTree.<<.service | string | `"tin"` |  |
| cdc.serviceTree.module | string | `"cdc"` |  |
| cdc.tolerations | list | `[]` |  |
| cdc.topologySpreadConstraints | object | `{}` |  |
| helium.bromine.enable | bool | `true` |  |
| helium.bromine.rootSecret.name | string | `"helium-doppler-secret"` |  |
| helium.bromine.target | string | `"nitroso-helium"` |  |
| helium.enable | bool | `false` |  |
| helium.fullnameOverride | string | `"helium-poller"` |  |
| livecache.nameOverride | string | `"tin-livecache"` |  |
| livecache.passwordFromSecret.enable | bool | `true` |  |
| livecache.passwordFromSecret.existingSecret.key | string | `"ATOMI_CACHE__LIVE__PASSWORD"` |  |
| livecache.passwordFromSecret.existingSecret.name | string | `"nitroso-tin"` |  |
| livecache.storage.enabled | bool | `false` |  |
| poller.affinity | object | `{}` |  |
| poller.annotations."argocd.argoproj.io/sync-wave" | string | `"4"` |  |
| poller.appSettings | object | `{}` |  |
| poller.autoscaling | object | `{}` |  |
| poller.configMountPath | string | `"/app/config"` |  |
| poller.enabled | bool | `true` |  |
| poller.envFromSecret | string | `"nitroso-tin"` |  |
| poller.image.pullPolicy | string | `"IfNotPresent"` |  |
| poller.image.repository | string | `"nitroso-tin-poller"` |  |
| poller.image.tag | string | `""` |  |
| poller.imagePullSecrets | list | `[]` |  |
| poller.jobRbac.annotations."argocd.argoproj.io/sync-wave" | string | `"4"` |  |
| poller.jobRbac.create | bool | `true` |  |
| poller.nameOverride | string | `"tin-poller"` |  |
| poller.nodeSelector | object | `{}` |  |
| poller.podAnnotations | object | `{}` |  |
| poller.podSecurityContext | object | `{}` |  |
| poller.replicaCount | int | `1` |  |
| poller.resources | object | `{}` |  |
| poller.securityContext | object | `{}` |  |
| poller.serviceAccount.annotations."argocd.argoproj.io/sync-wave" | string | `"4"` |  |
| poller.serviceAccount.create | bool | `true` |  |
| poller.serviceTree.<<.landscape | string | `"lapras"` |  |
| poller.serviceTree.<<.layer | string | `"2"` |  |
| poller.serviceTree.<<.platform | string | `"nitroso"` |  |
| poller.serviceTree.<<.service | string | `"tin"` |  |
| poller.serviceTree.module | string | `"poller"` |  |
| poller.tolerations | list | `[]` |  |
| poller.topologySpreadConstraints | object | `{}` |  |
| serviceTree.landscape | string | `"lapras"` |  |
| serviceTree.layer | string | `"2"` |  |
| serviceTree.platform | string | `"nitroso"` |  |
| serviceTree.service | string | `"tin"` |  |
| zinc.api.configMountPath | string | `"/app/Config"` |  |
| zinc.api.image.repository | string | `"ghcr.io/atomicloud/nitroso.zinc/api-arm"` |  |
| zinc.migration.enabled | bool | `false` |  |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.11.2](https://github.com/norwoodj/helm-docs/releases/v1.11.2)
