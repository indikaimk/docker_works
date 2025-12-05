require 'bundler/setup'
require 'docker_works'

# Load the Executor class explicitly for testing
require 'docker_works/runner'

RSpec.configure do |config|
  # Enable flags like --only-failures and --next-failure
  config.example_status_persistence_file_path = ".rspec_status"

  # Disable RSpec exposing methods globally on `Module` and `main`
  config.disable_monkey_patching!

  config.expect_with :rspec do |c|
    c.syntax = :expect
  end
  
  # Ensure the SSHKit output is muted during tests to prevent spamming the console
  config.before(:suite) do
    SSHKit.config.output = SSHKit::Formatter::BlackHole.new(STDOUT)
  end
end