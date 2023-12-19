# Sulfone Boron

Sulfone Boron is an executor designed to pull and run Docker images for a templating system called CyanPrint.

## Features

- Pulls Docker images
- Runs Docker images
- Integrated with CyanPrint templating system

## Usage

To use Sulfone Boron, ensure you have Docker installed and running on your system. Then, simply invoke Sulfone Boron with the appropriate command line arguments to pull and run your desired Docker image.

## Quick Start

#### Linux/OSX AMD
```bash
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock ghcr.io/atomicloud/sulfone.boron/sulfone-boron-amd:latest setup
docker run --rm -it -p 9000:9000 --network cyanprint -v /var/run/docker.sock:/var/run/docker.sock ghcr.io/atomicloud/sulfone.boron/sulfone-boron-amd:latest
```

#### Linux/OSX ARM
```bash
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock ghcr.io/atomicloud/sulfone.boron/sulfone-boron-arm:latest setup
docker run --rm -it -p 9000:9000 --network cyanprint -v /var/run/docker.sock:/var/run/docker.sock ghcr.io/atomicloud/sulfone.boron/sulfone-boron-arm:latest
```

#### Windows
```bash
docker run --rm -v //var/run/docker.sock:/var/run/docker.sock.sock ghcr.io/atomicloud/sulfone.boron/sulfone-boron-arm:latest setup
docker run --rm -it -p 9000:9000 --network cyanprint -v //var/run/docker.sock:/var/run/docker.sock ghcr.io/atomicloud/sulfone.boron/sulfone-boron-arm:latest
```

## Contributing

Contributions to Sulfone Boron are welcome. Please ensure that your code adheres to our coding standards and passes all tests before submitting a pull request.

## License

Sulfone Boron is licensed under the MIT License. See `LICENSE` for more information.
