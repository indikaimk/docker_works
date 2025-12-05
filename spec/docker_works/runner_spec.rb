require 'spec_helper'

# Helper class to mock the DSL config without loading a file
class MockDSL
  attr_reader :hosts, :tasks
  def initialize(hosts, tasks)
    @hosts = hosts
    @tasks = tasks
  end
end

RSpec.describe DockerWorks::Runner do
  let(:mock_hosts) { ["web01.test.com", "db01.test.com"] }
  let(:mock_tasks) do
    {
      deploy: [
        { type: :shell, command: "docker pull my-app:latest" },
        { type: :shell, command: "docker run -d my-app:latest" }
      ],
      failed_task: [
        { type: :shell, command: "true" },
        { type: :shell, command: "false_command" } # This command will fail in SSHKit
      ]
    }
  end
  let(:mock_dsl_config) { MockDSL.new(mock_hosts, mock_tasks) }
  subject(:executor) { DockerWorks::Runner.new(mock_dsl_config) }

  # We stub `on` and `within` which are inherited from SSHKit::DSL
  # This avoids actual network connections during unit tests.
  before do
    # Stub the SSHKit execution methods
    allow(executor).to receive(:on).and_yield(mock_hosts.first)
    allow(executor).to receive(:within).and_yield
  end

  describe '#execute_task' do
    context 'when the task is successful' do
      # Mock the SSHKit#execute method to return success for all commands
      before do
        allow(executor).to receive(:execute) 
        # Stub the output to keep tests clean
        allow(executor).to receive(:puts)
      end

      it 'calls SSHKit#on with correct hosts' do
        expect(executor).to receive(:on).with(mock_hosts, in: :parallel, limit: 5)
        executor.execute_task(:deploy)
      end

      it 'calls SSHKit#execute for all steps in the task' do
        executor.execute_task(:deploy)
        expect(executor).to have_received(:execute).with(:sh, '-c', 'docker pull my-app:latest')
        expect(executor).to have_received(:execute).with(:sh, '-c', 'docker run -d my-app:latest')
      end

      it 'reports task completion' do
        # Expect the output to contain the success message
        expect { executor.execute_task(:deploy) }.to output(/Task 'deploy' completed successfully/).to_stdout
      end
    end

    context 'when a command fails' do
      # Mock the second execute call to raise an SSHKit failure
      before do
        allow(executor).to receive(:execute) do |*args|
          # Only fail on the known failing command
          raise SSHKit::Command::Failed, "Remote command failed" if args.include?("false_command")
        end
      end
      
      it 'catches SSHKit::Command::Failed and reports the failure' do
        # Expect the output to contain the FATAL ERROR and the final FAILURE message
        output = -> { executor.execute_task(:failed_task) }
        expect(&output).to output(/FATAL ERROR on web01.test.com/).to_stdout
        # FIX: Check for the output containing the message raised by SSHKit::Runner::ExecuteError
        expect(&output).to output(/FAILURE: Deployment failed on web01.test.com/).to_stdout 
      end
    end

    context 'when the task is not found' do
      it 'reports an error' do
        expect { executor.execute_task(:non_existent_task) }.to output(/Error: Task 'non_existent_task' not found./).to_stdout
      end
    end
  end
end