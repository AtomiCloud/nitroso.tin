def start(landscape, platform, service, port, live):

    # build API image
    api_image_name = platform + "-" + service + "-consumer"
    docker_build(
        api_image_name,
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
