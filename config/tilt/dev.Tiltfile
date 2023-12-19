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

    enricher_image_name = platform + "-" + service + "-enricher"
    docker_build(
        enricher_image_name,
        '.',
        dockerfile = './infra/dev.Dockerfile',
        entrypoint='air -- enricher',
        live_update=[
            sync('.', '/app'),
        ]
    )

    reserver_image_name = platform + "-" + service + "-reserver"
    docker_build(
        reserver_image_name,
        '.',
        dockerfile = './infra/dev.Dockerfile',
        entrypoint='air -- reserver',
        live_update=[
            sync('.', '/app'),
        ]
    )

    buyer_image_name = platform + "-" + service + "-buyer"
    docker_build(
        buyer_image_name,
        '.',
        dockerfile = './infra/dev.Dockerfile',
        entrypoint='air -- buyer',
        live_update=[
            sync('.', '/app'),
        ]
    )