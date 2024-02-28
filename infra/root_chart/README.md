# root-chart

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 1.16.0](https://img.shields.io/badge/AppVersion-1.16.0-informational?style=flat-square)

Root Chart to a single Service

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| file://../consumer_chart | cdc(golang-chart) | 0.1.0 |
| file://../consumer_chart | poller(golang-chart) | 0.1.0 |
| file://../consumer_chart | enricher(golang-chart) | 0.1.0 |
| file://../consumer_chart | reserver(golang-chart) | 0.1.0 |
| file://../consumer_chart | buyer(golang-chart) | 0.1.0 |
| oci://ghcr.io/atomicloud/nitroso.helium | helium(root-chart) | 1.9.3 |
| oci://ghcr.io/atomicloud/nitroso.zinc | zinc(root-chart) | 1.18.0 |
| oci://ghcr.io/atomicloud/sulfoxide.bromine | bromine(sulfoxide-bromine) | 1.4.0 |
| oci://registry-1.docker.io/bitnamicharts | livecache(redis) | 18.6.1 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| bromine.annotations."argocd.argoproj.io/sync-wave" | string | `"1"` |  |
| bromine.enable | bool | `false` |  |
| bromine.rootSecret | object | `{"name":"nitroso-tin-doppler","ref":"NITROSO_TIN"}` | Secret of Secrets reference |
| bromine.rootSecret.ref | string | `"NITROSO_TIN"` | DOPPLER Token Reference |
| bromine.storeName | string | `"nitroso-tin"` | Store name to create |
| bromine.target | string | `"nitroso-tin"` |  |
| buyer.affinity | object | `{}` |  |
| buyer.annotations."argocd.argoproj.io/sync-wave" | string | `"4"` |  |
| buyer.appSettings.app.module | string | `"buyer"` |  |
| buyer.autoscaling | object | `{}` |  |
| buyer.configMountPath | string | `"/app/config"` |  |
| buyer.envFromSecret | string | `"nitroso-tin"` |  |
| buyer.image.pullPolicy | string | `"IfNotPresent"` |  |
| buyer.image.repository | string | `"nitroso-tin-buyer"` |  |
| buyer.image.tag | string | `""` |  |
| buyer.imagePullSecrets | list | `[]` |  |
| buyer.jobRbac.create | bool | `false` |  |
| buyer.nameOverride | string | `"tin-buyer"` |  |
| buyer.nodeSelector | object | `{}` |  |
| buyer.podAnnotations | object | `{}` |  |
| buyer.podSecurityContext | object | `{}` |  |
| buyer.replicaCount | int | `1` |  |
| buyer.resources | object | `{}` |  |
| buyer.securityContext | object | `{}` |  |
| buyer.serviceAccount.create | bool | `false` |  |
| buyer.serviceTree.<<.landscape | string | `"lapras"` |  |
| buyer.serviceTree.<<.layer | string | `"2"` |  |
| buyer.serviceTree.<<.platform | string | `"nitroso"` |  |
| buyer.serviceTree.<<.service | string | `"tin"` |  |
| buyer.serviceTree.module | string | `"buyer"` |  |
| buyer.tolerations | list | `[]` |  |
| buyer.topologySpreadConstraints | object | `{}` |  |
| cdc.affinity | object | `{}` |  |
| cdc.annotations."argocd.argoproj.io/sync-wave" | string | `"4"` |  |
| cdc.appSettings.app.module | string | `"cdc"` |  |
| cdc.autoscaling | object | `{}` |  |
| cdc.configMountPath | string | `"/app/config"` |  |
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
| cdc.serviceAccount.create | bool | `false` |  |
| cdc.serviceTree.<<.landscape | string | `"lapras"` |  |
| cdc.serviceTree.<<.layer | string | `"2"` |  |
| cdc.serviceTree.<<.platform | string | `"nitroso"` |  |
| cdc.serviceTree.<<.service | string | `"tin"` |  |
| cdc.serviceTree.module | string | `"cdc"` |  |
| cdc.stateful | bool | `false` |  |
| cdc.tolerations | list | `[]` |  |
| cdc.topologySpreadConstraints | object | `{}` |  |
| enricher.affinity | object | `{}` |  |
| enricher.annotations."argocd.argoproj.io/sync-wave" | string | `"4"` |  |
| enricher.appSettings.app.module | string | `"enricher"` |  |
| enricher.autoscaling | object | `{}` |  |
| enricher.configMountPath | string | `"/app/config"` |  |
| enricher.envFromSecret | string | `"nitroso-tin"` |  |
| enricher.image.pullPolicy | string | `"IfNotPresent"` |  |
| enricher.image.repository | string | `"nitroso-tin-enricher"` |  |
| enricher.image.tag | string | `""` |  |
| enricher.imagePullSecrets | list | `[]` |  |
| enricher.jobRbac.create | bool | `false` |  |
| enricher.nameOverride | string | `"tin-enricher"` |  |
| enricher.nodeSelector | object | `{}` |  |
| enricher.podAnnotations | object | `{}` |  |
| enricher.podSecurityContext | object | `{}` |  |
| enricher.replicaCount | int | `1` |  |
| enricher.resources | object | `{}` |  |
| enricher.securityContext | object | `{}` |  |
| enricher.serviceAccount.create | bool | `false` |  |
| enricher.serviceTree.<<.landscape | string | `"lapras"` |  |
| enricher.serviceTree.<<.layer | string | `"2"` |  |
| enricher.serviceTree.<<.platform | string | `"nitroso"` |  |
| enricher.serviceTree.<<.service | string | `"tin"` |  |
| enricher.serviceTree.module | string | `"enricher"` |  |
| enricher.stateful | bool | `false` |  |
| enricher.tolerations | list | `[]` |  |
| enricher.topologySpreadConstraints | object | `{}` |  |
| helium.bromine.enable | bool | `true` |  |
| helium.bromine.rootSecret.name | string | `"helium-doppler-secret"` |  |
| helium.bromine.target | string | `"nitroso-helium"` |  |
| helium.enable | bool | `false` |  |
| helium.fullnameOverride | string | `"helium-poller"` |  |
| livecache.architecture | string | `"standalone"` |  |
| livecache.auth.enabled | bool | `true` |  |
| livecache.auth.existingSecret | string | `"nitroso-tin"` |  |
| livecache.auth.existingSecretPasswordKey | string | `"ATOMI_CACHE__LIVE__PASSWORD"` |  |
| livecache.commonAnnotations."argocd.argoproj.io/sync-wave" | string | `"2"` |  |
| livecache.commonAnnotations."atomi.cloud/module" | string | `"livecache"` |  |
| livecache.commonAnnotations.<<."atomi.cloud/layer" | string | `"2"` |  |
| livecache.commonAnnotations.<<."atomi.cloud/platform" | string | `"nitroso"` |  |
| livecache.commonAnnotations.<<."atomi.cloud/service" | string | `"tin"` |  |
| livecache.commonLabels."atomi.cloud/module" | string | `"livecache"` |  |
| livecache.commonLabels.<<."atomi.cloud/layer" | string | `"2"` |  |
| livecache.commonLabels.<<."atomi.cloud/platform" | string | `"nitroso"` |  |
| livecache.commonLabels.<<."atomi.cloud/service" | string | `"tin"` |  |
| livecache.master.persistence.enabled | bool | `false` |  |
| livecache.nameOverride | string | `"tin-livecache"` |  |
| livecache.podAnnotations."argocd.argoproj.io/sync-wave" | string | `"2"` |  |
| livecache.podAnnotations."atomi.cloud/module" | string | `"livecache"` |  |
| livecache.podAnnotations.<<."atomi.cloud/layer" | string | `"2"` |  |
| livecache.podAnnotations.<<."atomi.cloud/platform" | string | `"nitroso"` |  |
| livecache.podAnnotations.<<."atomi.cloud/service" | string | `"tin"` |  |
| livecache.replica.persistence.enabled | bool | `false` |  |
| livecache.resources.limits.cpu | string | `"250m"` |  |
| livecache.resources.limits.memory | string | `"512Mi"` |  |
| livecache.resources.requests.cpu | string | `"100m"` |  |
| livecache.resources.requests.memory | string | `"256Mi"` |  |
| poller.affinity | object | `{}` |  |
| poller.annotations."argocd.argoproj.io/sync-wave" | string | `"4"` |  |
| poller.appSettings.app.module | string | `"poller"` |  |
| poller.autoscaling | object | `{}` |  |
| poller.configMountPath | string | `"/app/config"` |  |
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
| poller.stateful | bool | `false` |  |
| poller.tolerations | list | `[]` |  |
| poller.topologySpreadConstraints | object | `{}` |  |
| reserver.affinity | object | `{}` |  |
| reserver.annotations."argocd.argoproj.io/sync-wave" | string | `"4"` |  |
| reserver.appSettings.app.module | string | `"reserver"` |  |
| reserver.autoscaling | object | `{}` |  |
| reserver.configMountPath | string | `"/app/config"` |  |
| reserver.envFromSecret | string | `"nitroso-tin"` |  |
| reserver.env[0].name | string | `"ATOMI_RESERVER__GROUP"` |  |
| reserver.env[0].valueFrom.fieldRef.fieldPath | string | `"metadata.name"` |  |
| reserver.image.pullPolicy | string | `"IfNotPresent"` |  |
| reserver.image.repository | string | `"nitroso-tin-reserver"` |  |
| reserver.image.tag | string | `""` |  |
| reserver.imagePullSecrets | list | `[]` |  |
| reserver.jobRbac.create | bool | `false` |  |
| reserver.nameOverride | string | `"tin-reserver"` |  |
| reserver.nodeSelector | object | `{}` |  |
| reserver.podAnnotations | object | `{}` |  |
| reserver.podSecurityContext | object | `{}` |  |
| reserver.replicaCount | int | `1` |  |
| reserver.resources | object | `{}` |  |
| reserver.securityContext | object | `{}` |  |
| reserver.serviceAccount.create | bool | `false` |  |
| reserver.serviceTree.<<.landscape | string | `"lapras"` |  |
| reserver.serviceTree.<<.layer | string | `"2"` |  |
| reserver.serviceTree.<<.platform | string | `"nitroso"` |  |
| reserver.serviceTree.<<.service | string | `"tin"` |  |
| reserver.serviceTree.module | string | `"reserver"` |  |
| reserver.stateful | bool | `true` |  |
| reserver.tolerations | list | `[]` |  |
| reserver.topologySpreadConstraints | object | `{}` |  |
| serviceTree.landscape | string | `"lapras"` |  |
| serviceTree.layer | string | `"2"` |  |
| serviceTree.platform | string | `"nitroso"` |  |
| serviceTree.service | string | `"tin"` |  |
| tags."atomi.cloud/layer" | string | `"2"` |  |
| tags."atomi.cloud/platform" | string | `"nitroso"` |  |
| tags."atomi.cloud/service" | string | `"tin"` |  |
| zinc.api.configMountPath | string | `"/app/Config"` |  |
| zinc.api.image.repository | string | `"ghcr.io/atomicloud/nitroso.zinc/api"` |  |
| zinc.migration.enabled | bool | `false` |  |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.11.2](https://github.com/norwoodj/helm-docs/releases/v1.11.2)
