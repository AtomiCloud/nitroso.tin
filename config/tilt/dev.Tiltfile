def start(landscape, platform, service, port, live):

    # Build API
    api_image_name = platform + "-" + service + "-consumer"
    docker_build(
        api_image_name,
        '.',
        dockerfile = './infra/dev.Dockerfile',
        entrypoint='air -- start',
        live_update=[
            sync('.', '/app'),
        ]
    )