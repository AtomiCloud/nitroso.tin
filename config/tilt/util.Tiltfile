def helm_watch(landscape, platform, service, t):

    ns = platform
    release = platform + "-" + service

    setApi = ''
    if t == 'local':
        setApi = 'api.enabled=false'
    elif t == 'cluster':
        setApi = 'api.enabled=true'
    else:
        fail('Unknown type: ' + t)
    watch_file('infra/api_chart')
    watch_file('infra/migration_chart')
    watch_file('infra/root_chart')
    


    local_resource(
        'update-build-helm',
        cmd='helm dependency update ./infra/root_chart',
        deps=[
            './infra/root_chart/Chart.lock',
            './infra/chart/**/*.*'
        ]
    )

    local_resource(
        'sync-configuration',
        cmd='./scripts/local/config-sync.sh',
        deps = [
            './App/Config/**/*.*',
        ]
    )   
    
    k8s_yaml(
        helm('./infra/root_chart',
            name = release,
            namespace = ns,
            values = [
                './infra/root_chart/values.yaml',
                './infra/root_chart/values.' + landscape + '.yaml',
            ],
            set = [
                setApi
            ]

        )
    )