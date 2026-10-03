# Use a saved playlist for direct playback

Setup saves a selected library playlist by persistent ID so `homepodctl play` can use saved playlist and room preferences without another target argument. Explicit playlist targets override this preference; explicit empty or conflicting targets still fail, and aliases and automation retain their own target requirements. Execution validates the saved ID before playback changes, while previews retain their existing lookup-free behavior, so a deleted default cannot silently change outputs or select a different playlist.
