# DotEnv

A simple and flexible Go library for reading environment variables from a `.env` file.

In all environments, the following files are loaded if they exist, the latter taking precedence over the former:
* .env                contains default values for the environment variables needed by the app
* .env.local          uncommitted file with local overrides
* .env.$APP_ENV       committed environment-specific defaults
* .env.$APP_ENV.local uncommitted environment-specific overrides

Real environment variables win over .env files.
