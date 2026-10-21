import express from "express";
import { readFile } from "fs/promises";
import { dirname } from "path";
import { fileURLToPath } from "url";
import fetch from "node-fetch";

const app = express();
const port = 3000;
const __dirname = dirname(fileURLToPath(import.meta.url));

// Serve static files from the current directory
app.use(express.static(__dirname));

// Read product data from JSON file
let data;
try {
  data = JSON.parse(await readFile("./Products.json", "utf-8"));
} catch (error) {
  console.error("Failed to read Products.json:", error);
  data = [];
}

// Endpoint to serve product data
app.get("/Products", (req, res) => {
  res.json(data);
});

// Endpoint to fetch weather data
app.get("/weather", async (req, res) => {
  const apiURL =
    "https://api.open-meteo.com/v1/forecast?latitude=-6.2088&longitude=106.8456&current_weather=true";
  try {
    const apiResponse = await fetch(apiURL);
    const weatherData = await apiResponse.json();
    res.json(weatherData.current_weather);
  } catch (error) {
    console.error("Failed to fetch weather data:", error);
    res.status(500).send("Failed to fetch weather data");
  }
});

// Start the server
app.listen(port, () => {
  console.log(`Server running on http://localhost:${port}`);
});
