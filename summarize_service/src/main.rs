use actix_web::{web, App, HttpResponse, HttpServer, Responder};
use serde::{Deserialize, Serialize};
use std::sync::Mutex;
use reqwest::Client;

#[derive(Deserialize)]
struct Info {
    file_title: String,
}

#[derive(Serialize)]
struct TextGenerationResult {
    explanation: String,
}

struct AppState {
    client: Client,
    huggingface_token: String,
}





#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let client = Client::new();
    let huggingface_token = "".to_string();
    let app_state = web::Data::new(Mutex::new(AppState { client, huggingface_token }));

    HttpServer::new(move || {
        App::new()
            .app_data(app_state.clone())
            
    })
    .bind("127.0.0.1:8080")?
    .run()
    .await
}

