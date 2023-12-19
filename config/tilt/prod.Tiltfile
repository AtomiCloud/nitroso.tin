def start(landscape, platform, service, port, live):

    # build API image
    cdc_image_name = platform + "-" + service + "-cdc"
    docker_build(
        cdc_image_name,
        '.',
        dockerfile = './infra/Dockerfile',
    )

    poller_image_name = platform + "-" + service + "-poller"
    docker_build(
        poller_image_name,
        '.',
        dockerfile = './infra/Dockerfile',
    )

    # Add Link
    k8s_resource(
       workload = api_image_name,
       links=[
         link('http://api.' + service + '.' + platform + '.' + landscape +  '.lvh.me:' + str(port) + '/swagger/index.html','API')
       ]
    )
