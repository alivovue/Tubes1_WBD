document.addEventListener("DOMContentLoaded", async () => {
  const weatherDiv = document.querySelector(".weather");

  try {
    const response = await fetch("/weather");
    const weather = await response.json();
    weatherDiv.innerHTML = `
          Temperature: ${weather.temperature}°C<br>
          Wind Speed: ${weather.windspeed} km/h
      `;
  } catch (error) {
    weatherDiv.textContent = "Failed to fetch weather data.";
  }
});
