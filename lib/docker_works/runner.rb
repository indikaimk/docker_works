require 'sshkit'
require 'sshkit/dsl'
require_relative 'dsl' # Assuming the DSL file is in the parent directory

module DockerWorks
  class Runner
    include SSHKit::DSL

    def initialize(dsl_config)
      @config = dsl_config
      configure_sshkit
    end

    # Public entry point to run a specific task.
    def execute_task(task_name)
      steps = @config.tasks[task_name.to_sym]
      
      unless steps
        puts "Error: Task '#{task_name}' not found."
        return
      end

      puts "Starting workflow for task: #{task_name}"
      
      # SSHKit `on` block runs the commands on all defined hosts.
      # We use :parallel to deploy containers concurrently on all servers.
      on @config.hosts, in: :parallel, limit: 5 do |host|
        puts "\n--- Executing Task Steps on #{host} ---"
        
        steps.each do |step|
          cmd = step[:command]
          type = step[:type]
          
          begin
            # The SSHKit::DSL#execute method is used to run the command on the remote host.
            # We use `with` to set a working directory (optional, but good practice).
            within "/home/deployer/app_source" do
              puts "  [#{type.to_s.upcase}] #{cmd}"
              # `capture` executes the command and captures the output.
              capture(:sh, '-c', cmd)
            end
            
          rescue SSHKit::Command::Failed => e
            puts "Command failed on #{host} (#{type}): #{e.message}"
            # Log and halt deployment on this specific host
            raise SSHKit::Runner::ExecuteError, "Deployment failed on #{host} during step: #{cmd}" 
          end
        end
        puts "--- Task '#{task_name}' completed successfully on #{host} ---"
      end

    rescue SSHKit::Runner::ExecuteError => e
      puts "\nFAILURE: #{e.message}"
    end

    private

    # Sets up SSHKit configuration for better output.
    def configure_sshkit
      SSHKit.configure do |config|
        # Use the pretty formatter to clearly show output from multiple hosts.
        config.output = SSHKit::Formatter::Pretty.new($stdout)
        
        # Configure the SSH transport (e.g., set user, keys)
        # config.sshkit.backend = SSHKit::Backend::Netssh # Default is fine
        # config.sshkit.ssh_options = { user: 'deployer', keys: ['~/.ssh/id_rsa'] } 
      end
    end
  end
end