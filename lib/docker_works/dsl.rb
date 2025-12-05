require 'sshkit/dsl'

module DockerWorks
  # This class is responsible for loading and parsing the workflow definition file (e.g., Workflow.rb).
  # All DSL methods (like `hosts`, `task`, `build_image`) are defined here.
  class DSL
    attr_reader :hosts, :tasks, :current_task_name

    def initialize
      @hosts = []
      @tasks = {}
      @current_task_name = nil
    end

    # Parses and evaluates the workflow file.
    def self.load(file_path)
      dsl = new
      # Use instance_eval to execute the file contents in the context of this DSL object,
      # making methods like `hosts` and `task` available globally within the file.
      dsl.instance_eval(File.read(file_path), file_path, 1)
      dsl
    end

    # DSL method: Define target hosts.
    # Usage: hosts "web01.example.com", "web02.example.com"
    def hosts(*host_names)
      @hosts.concat(host_names.flatten.compact.map(&:to_s))
    end

    # DSL method: Define a workflow task block.
    # Usage: task :build do ... end
    def task(name, &block)
      @current_task_name = name
      @tasks[name] = []
      # Evaluate the task block immediately to store the steps (build_image, execute, etc.)
      instance_exec(&block)
    ensure
      @current_task_name = nil
    end

    # DSL command: Docker Build.
    # Stores the Docker command string to be executed later.
    def build_image(image_name, path: '.', tag: 'latest')
      command = "docker build -t #{image_name}:#{tag} #{path}"
      store_command(:build, command, image: image_name, tag: tag)
    end
    
    # DSL command: Docker Run.
    def run_container(image_name, name:, port:, detach: true)
      flags = "#{detach ? '-d' : ''} -p #{port} --name #{name}"
      command = "docker run #{flags} #{image_name}"
      store_command(:run, command, name: name, image: image_name)
    end
    
    # Generic DSL command: Execute any shell command.
    def execute(command)
      store_command(:shell, command)
    end

    private

    # Stores the command hash in the current task's step list.
    def store_command(type, command_string, options = {})
      raise "Commands must be defined inside a `task` block." unless @current_task_name
      @tasks[@current_task_name] << { type: type, command: command_string, options: options }
    end
  end
end