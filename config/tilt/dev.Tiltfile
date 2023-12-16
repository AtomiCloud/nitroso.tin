def start(landscape, platform, service, port, live):


    cdc_image_name = platform + "-" + service + "-cdc"
    docker_build(
        cdc_image_name,
        '.',
        dockerfile = './infra/dev.Dockerfile',
        entrypoint='air -- cdc',
        live_update=[
            sync('.', '/app'),
        ]
    )

    poller_image_name = platform + "-" + service + "-poller"
    docker_build(
        poller_image_name,
        '.',
        dockerfile = './infra/dev.Dockerfile',
        entrypoint='air -- poller',
        live_update=[
            sync('.', '/app'),
        ]
    )