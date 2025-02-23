require_relative '../boot'
require 'minitest/autorun'
require 'faye/websocket'
require 'eventmachine'
require 'pry-rescue/minitest' unless ENV['CI']

describe "A folder" do
  it "can be shared to a recipient in sync mode" do
    Helpers.scenario "sync_folder"
    Helpers.start_mailhog

    recipient_name = "Bob"

    # Create the instances
    inst = Instance.create name: "Alice"
    inst_recipient = Instance.create name: recipient_name

    # Create the folders
    folder = Folder.create inst
    folder.couch_id.wont_be_empty
    child1 = Folder.create inst, dir_id: folder.couch_id
    file = "../fixtures/wet-cozy_20160910__M4Dz.jpg"
    opts = CozyFile.options_from_fixture(file, dir_id: folder.couch_id)
    file = CozyFile.create inst, opts
    zip  = "../fixtures/logos.zip"
    opts = CozyFile.options_from_fixture(zip, dir_id: child1.couch_id)
    CozyFile.create inst, opts

    # Create the sharing of a folder
    contact = Contact.create inst, given_name: recipient_name
    sharing = Sharing.new
    sharing.rules << Rule.sync(folder)
    sharing.members << inst << contact
    inst.register sharing

    # Manually set the xor_key
    doc = Helpers.couch.get_doc inst.domain, Sharing.doctype, sharing.couch_id
    key = Sharing.make_xor_key
    doc["credentials"][0]["xor_key"] = key
    Helpers.couch.update_doc inst.domain, Sharing.doctype, doc

    # Accept the sharing and check the realtime events to see when files are synchronized
    EM.run do
      ws = Faye::WebSocket::Client.new("ws://#{inst_recipient.domain}/realtime/")

      ws.on :open do
        ws.send({
          method: "AUTH",
          payload: inst_recipient.token_for("io.cozy.files")
        }.to_json)
        ws.send({
          method: "SUBSCRIBE",
          payload: { type: "io.cozy.sharings.initial_sync", id: sharing.couch_id }
        }.to_json)
      end

      ws.on :message do |event|
        msg = JSON.parse(event.data)
        ws.close if msg["event"] == "DELETED"
      end

      ws.on :close do
        EM.stop
      end

      # Add a timeout after 1 minute to make the tests more reliable on travis
      EM::Timer.new(60) do
        EM.stop
      end

      sleep 1
      inst_recipient.accept sharing
      doc = Helpers.couch.get_doc inst_recipient.domain, Sharing.doctype, sharing.couch_id
      assert_equal 2, doc["initial_number_of_files_to_sync"]
    end
    sleep 1
    doc = Helpers.couch.get_doc inst_recipient.domain, Sharing.doctype, sharing.couch_id
    assert_nil doc["initial_number_of_files_to_sync"]

    # Check the folders are the same
    child1_path = "/#{Helpers::SHARED_WITH_ME}/#{folder.name}/#{child1.name}"
    child1_recipient = Folder.find_by_path inst_recipient, child1_path
    child1_id_recipient = child1_recipient.couch_id
    folder_id_recipient = child1_recipient.dir_id
    file_path = "/#{Helpers::SHARED_WITH_ME}/#{folder.name}/#{file.name}"
    file_recipient = CozyFile.find_by_path inst_recipient, file_path
    file_id_recipient = file_recipient.couch_id
    assert_equal child1.name, child1_recipient.name
    assert_equal file.name, file_recipient.name

    # Check the sync (create + update) sharer -> recipient
    child1.rename inst, Faker::Internet.slug
    child2 = Folder.create inst, dir_id: folder.couch_id
    child1.move_to inst, child2.couch_id
    opts = CozyFile.metadata_options_for(inst, label: Faker::Simpsons.quote)
    opts[:mime] = 'text/plain'
    file.overwrite inst, opts
    file.rename inst, "#{Faker::Internet.slug}.txt"
    sleep 12

    child1_recipient = Folder.find inst_recipient, child1_id_recipient
    child2_path = "/#{Helpers::SHARED_WITH_ME}/#{folder.name}/#{child2.name}"
    child2_recipient = Folder.find_by_path inst_recipient, child2_path
    file = CozyFile.find inst, file.couch_id
    file_recipient = CozyFile.find inst_recipient, file_id_recipient
    assert_equal child1.name, child1_recipient.name
    assert_equal child2.name, child2_recipient.name
    assert_equal child1_recipient.dir_id, child2_recipient.couch_id
    assert_equal child1.cozy_metadata, child1_recipient.cozy_metadata
    assert_equal file.name, file_recipient.name
    assert_equal file.md5sum, file_recipient.md5sum
    assert_equal file.couch_rev, file_recipient.couch_rev
    assert_equal file.metadata, file_recipient.metadata
    assert_equal file.cozy_metadata, file_recipient.cozy_metadata

    # Check the sync (create + update) recipient -> sharer
    child1_recipient.rename inst_recipient, Faker::Internet.slug
    child3_recipient = Folder.create inst_recipient, dir_id: folder_id_recipient
    child1_recipient.move_to inst_recipient, child3_recipient.couch_id
    file_recipient.rename inst_recipient, "#{Faker::Internet.slug}.txt"
    file_recipient.overwrite inst_recipient, content: "New content from recipient"
    sleep 3
    note_recipient = Note.create inst_recipient, dir_id: child3_recipient.couch_id

    sleep 12
    child1 = Folder.find inst, child1.couch_id
    child3_path = "/#{folder.name}/#{child3_recipient.name}"
    child3 = Folder.find_by_path inst, child3_path
    file = CozyFile.find inst, file.couch_id
    assert_equal child1_recipient.name, child1.name
    assert_equal child3_recipient.name, child3.name
    assert_equal child1.dir_id, child3.couch_id
    assert_equal file_recipient.name, file.name
    assert_equal file_recipient.md5sum, file.md5sum
    assert_equal file_recipient.couch_rev, file.couch_rev

    note_path = "/#{folder.name}/#{child3.name}/#{note_recipient.file.name}"
    note = CozyFile.find_by_path inst, note_path
    parameters = Note.open inst, note.couch_id
    assert_equal note_recipient.file.couch_id, parameters["note_id"]
    assert %w[flat nested].include? parameters["subdomain"]
    assert %w[http https].include? parameters["protocol"]
    assert_equal inst_recipient.domain, parameters["instance"]
    refute_nil parameters["sharecode"]
    assert_equal "Alice", parameters["public_name"]

    # Check that the files are the same on disk
    da = File.join Helpers.current_dir, inst.domain, folder.name
    db = File.join Helpers.current_dir, inst_recipient.domain,
                   Helpers::SHARED_WITH_ME, sharing.rules.first.title
    Helpers.fsdiff(da, db).must_be_empty

    # Create a new folder
    child2 = Folder.create inst, dir_id: folder.couch_id

    # Create a folder on the recipient side, with a fixed id being the
    # xor_id of the child2 folder
    name = Faker::Internet.slug
    doc = {
      type: "directory",
      name: name,
      dir_id: Folder::ROOT_DIR,
      path: "/#{name}",
      created_at: "2018-05-11T12:18:37.558389182+02:00",
      updated_at: "2018-05-11T12:18:37.558389182+02:00"
    }
    id = Sharing.xor_id(child2.couch_id, key)
    Helpers.couch.create_named_doc inst_recipient.domain, Folder.doctype, id, doc

    # Make an update
    child2.rename inst, Faker::Internet.slug
    sleep 4

    # The child1 folder shouldn't be part of the sharing as its id exists
    # on the recipient side
    child2_recipient = Folder.find inst_recipient, id
    assert(child2.name != child2_recipient.name)
    path = File.join Helpers.current_dir, inst_recipient.domain,
                     Helpers::SHARED_WITH_ME, sharing.rules.first.title,
                     child2_recipient.name
    assert !Helpers.file_exists_in_fs(path)

    # Create the sharing of a file
    file = "../fixtures/wet-cozy_20160910__M4Dz.jpg"
    opts = CozyFile.options_from_fixture(file, dir_id: Folder::ROOT_DIR)
    file = CozyFile.create inst, opts
    sharing = Sharing.new
    sharing.rules << Rule.sync(file)
    sharing.members << inst << contact
    inst.register sharing
    sleep 1

    # On the second sharing with the same recipient, a shortcut should have
    # been sent to their cozy instance
    shortcut_path = "/#{Helpers::SHARED_WITH_ME}/#{sharing.description}.url"
    shortcut = CozyFile.find_by_path inst_recipient, shortcut_path
    assert_equal shortcut.metadata["sharing"]["status"], "new"
    target = shortcut.metadata["target"]
    assert_equal target["_type"], "io.cozy.files"
    assert_equal target["mime"], "image/jpeg"
    assert_equal target["cozyMetadata"]["instance"], "http://#{inst.domain}"

    # Accept the sharing
    inst_recipient.accept sharing
    sleep 12

    # Check the files are the same
    file = CozyFile.find inst, file.couch_id
    file_path = "/#{Helpers::SHARED_WITH_ME}/#{file.name}"
    file_recipient = CozyFile.find_by_path inst_recipient, file_path
    file_id_recipient = file_recipient.couch_id
    assert_equal file.name, file_recipient.name
    assert_equal file.couch_rev, file_recipient.couch_rev

    # Check the sync sharer -> recipient
    file.overwrite inst, mime: 'text/plain'
    file.rename inst, "#{Faker::Internet.slug}.txt"
    sleep 12
    file = CozyFile.find inst, file.couch_id
    file_recipient = CozyFile.find inst_recipient, file_id_recipient
    assert_equal file.name, file_recipient.name
    assert_equal file.md5sum, file_recipient.md5sum
    assert_equal file.couch_rev, file_recipient.couch_rev

    # Check the sync recipient -> sharer
    file_recipient.rename inst_recipient, "#{Faker::Internet.slug}.txt"
    file_recipient.overwrite inst_recipient, content: "New content from recipient"
    sleep 12
    file = CozyFile.find inst, file.couch_id
    file_recipient = CozyFile.find inst_recipient, file_id_recipient
    assert_equal file.name, file_recipient.name
    assert_equal file.md5sum, file_recipient.md5sum
    assert_equal file.couch_rev, file_recipient.couch_rev

    assert_equal inst.check, []
    # XXX we don't check the FS of inst_recipient as we have played directly with CouchDB on it

    inst.remove
    inst_recipient.remove
  end
end
