package inventory

all: {
    vars: {
        env: "dev"
    }
    children: {
        webservers: {
            hosts: {
                web1: {
                    host: "10.0.0.10"
                    user: "deploy"
                    port: 22
                }
                web2: {
                    host: "10.0.0.11"
                    user: "deploy"
                }
            }
            vars: {
                http_port: 8080
            }
        }
        dbservers: {
            hosts: {
                db1: {
                    host: "10.0.0.20"
                    user: "deploy"
                }
            }
            vars: {
                db_port: 5432
            }
        }
    }
}
