// Orbit's .NET e2e fixture: the smallest web app that answers a health check
// on the port Orbit injects.
var app = WebApplication.CreateBuilder(args).Build();
app.MapGet("/health", () => "ok");
app.Run($"http://127.0.0.1:{Environment.GetEnvironmentVariable("PORT")}");
