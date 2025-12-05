require 'spec_helper'

RSpec.describe DockerWorks::DSL do
  # Define the constant file path outside of let to ensure it's available in before(:all)
  WORKFLOW_FILE = 'workflow.rb'

  # The test setup moves to before(:all) using instance variables to avoid the
  # 'let/subject called in before(:context)' warning.
  before(:all) do
    # 1. Define the test file content
    test_content = %Q{
      hosts "host1", "host2"

      task :test_task do
        # Testing different DSL methods
        build_image "api", path: '.', tag: 'dev'
        run_container "api:dev", name: "api-c", port: "3000:3000", detach: false
        execute "ls -l"
      end
    }
    
    # 2. Write the file to the disk
    File.write(WORKFLOW_FILE, test_content)
    
    # 3. Load the configuration once and store it in an instance variable
    @dsl_config = DockerWorks::DSL.load(WORKFLOW_FILE)
  end

  after(:all) do
    # 4. Clean up the created file
    File.delete(WORKFLOW_FILE) if File.exist?(WORKFLOW_FILE)
  end

  # Use subject to access the configuration loaded in before(:all)
  # This pattern correctly exposes the @dsl_config instance variable for use in examples.
  subject(:dsl_config) { @dsl_config }

  it 'correctly loads and stores hosts' do
    expect(dsl_config.hosts).to eq(['host1', 'host2'])
  end

  it 'correctly loads and stores tasks' do
    expect(dsl_config.tasks.keys).to include(:test_task)
  end

  describe 'task parsing' do
    let(:steps) { dsl_config.tasks[:test_task] }

    it 'stores three steps' do
      expect(steps.length).to eq(3)
    end

    it 'correctly translates build_image into a command string' do
      build_step = steps[0]
      expect(build_step[:type]).to eq(:build)
      expect(build_step[:command]).to eq('docker build -t api:dev .')
      expect(build_step[:options][:image]).to eq('api')
    end

    it 'correctly translates run_container into a command string with detach=false' do
      run_step = steps[1]
      expect(run_step[:type]).to eq(:run)
      # Detach is false, so it should NOT include -d flag. Note the double space after 'run'
      expect(run_step[:command]).to eq('docker run  -p 3000:3000 --name api-c api:dev')
    end

    it 'correctly stores a generic execute command' do
      execute_step = steps[2]
      expect(execute_step[:type]).to eq(:shell)
      expect(execute_step[:command]).to eq('ls -l')
    end
  end
end