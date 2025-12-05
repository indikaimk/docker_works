# frozen_string_literal: true

# require "bundler/gem_tasks"
# require "minitest/test_task"

# Minitest::TestTask.create

# require "rubocop/rake_task"

# RuboCop::RakeTask.new

# task default: %i[test rubocop]

require 'rake/testtask'
require 'rspec/core/rake_task'

# Define a task to run RSpec tests
RSpec::Core::RakeTask.new(:spec)

# Set the default task to run tests
task :default => :spec
